# Operational Location Convention: Park + Shed + Partition

**Status:** Accepted (2026-08-06)  
**Guard:** `make operational-location-guard`  
**Owned by:** Maintainer (STANDING LOCK)

---

## Problem Statement

Goat OS divides physical barns into partitions to track animals at finer granularity than raw shed buildings. Early implementations diverged on how to name, store, display, and query these locations:

- Some code normalized `Castro 1` and `Castro 2` to a single shed `Castro` with partitions, others treated them as separate physical sheds.
- Name-based grouping silently merged animals across parks (two `Castro` sheds exist — one in Mandela, one in Godel).
- Three production data sources — the Mesha master registry, BigQuery, and legacy operator CSVs — each used a different shape (`Godel 1 - Part 3`, `GODEL 1 - PART 4`, `Godel 1 1`).
- The term "active shed" was used to mean both "a physical building" and "a building currently holding animals", blurring storage and product semantics.
- Six query paths had already drifted from the canonical composition rule, producing bugs like `Godel 1 1` (naive space-numeric join) and duplicate `Godel 1` rows in UI selectors.

**Evidence gathered 2026-08-06 from three independent sources:**

| Source | Format | Example | Scale |
|--------|--------|---------|-------|
| **Master Registry** (`Sheds DB.xlsx`) | Text rows in Mesha wiki | `Godel 1 - Part 1` … `Godel 1 - Part 10` | One row per physical partition |
| **Live BigQuery** (`goatos-sheets`) | Tables `counting.counting_db_view`, `Shiftings.shiftings_fact` | `MANDELA 1 - PART 3` (639 animals), `GODEL 1 - PART 4` (450) | ~100:1 dashed form usage |
| **Legacy Production Code** | Hardcoded constants in feed automation + dashboard UI | `slack-automation-scripts/feed_automation.js`: `'Mandela 2 - Part 3'`; `dashboard/.../SHED_CAPACITIES`: `"Gandhi 1 - Part 1"`, `"Castro 1"` | Both numeric and dashed names in prod |

---

## Convention: Unified Definition

The ground location of any goat is a three-part identity:

```
OperationalLocation = park + physical_shed + optional partition_label
```

**Three rules form the complete convention:**

### Rule 1: Storage Normalization (Backend)

Normalize partition labels at the database layer. The `locations` table stores one row per PHYSICAL LOCATION:

- **Subdivided sheds** (historically named with a number suffix like `Godel 1`, `Mandela 2`) → normalized to `shed_name` + `partition_label`
  - `Castro 1`, `Castro 2`, `Castro 3` → ONE shed `Castro` with partitions `1`, `2`, `3`
  - `Godel 1 - Part 3` → ONE shed `Godel 1` with partition `Part 3`
  - `Mandela 2 - Part 1` → ONE shed `Mandela 2` with partition `Part 1`

- **Undivided sheds** (single-name or numeric-suffix sheds that are NOT subdivided) → stored with NULL / '' / 'whole' partition
  - `Yashoda` → `Yashoda` + NULL partition
  - `Ho Chi Minh 1` → `Ho Chi Minh 1` + NULL partition (the `1` is part of the shed name, NOT a partition number)

- **Key insight:** Short-numeric names like `Castro`, `Gandhi`, `Yashoda` ARE NOT partitions. Only `Godel`, `Mandela`, `Sumathi`, and the `Gandhi`-family (subdivided variants) take a partition.

**Normalization happens at SEED/IMPORT time,** not at query time. The `locations` table is the SSOT for partition existence.

### Rule 2: Product Display (All Surfaces)

User-facing surfaces ALWAYS show the partition when one exists:

- No partition (NULL / '' / 'whole') → Display the plain shed name: `Yashoda`, `Castro 1`, `Ho Chi Minh 1`
- Has partition → Display `shed_name - partition_label`: `Godel 1 - Part 3`, `Mandela 2 - Part 1`

**NEVER render:**
- `Yashoda whole` — `'whole'` is a matching key for queries, never copy for users
- `Godel 1 1` — result of naive space-numeric join (worked example of what breaks)
- Shed name alone when a partition exists (e.g., `Godel 1` without the partition) — **always carry both halves**

**Both halves must always be read together.** Rendering the display requires BOTH `shed_id` (and its display name) AND `partition_label` in the response struct.

### Rule 3: Composition Location (Code)

Shared location-composition logic lives in ONE place per language — use it instead of hand-rolling:

- **Go:** `backend/internal/platform/oploc` — provides `DisplayName(shed, partition)`, querying helpers, and constants
- **Admin-web:** `apps/admin-web/lib/operational-location.ts` — TypeScript helpers for display and filtering
- **Android:** `core/core-ui/.../PartitionLabel.kt` — Kotlin composable for rendering the partition label and full location

Hand-rolled copies drift. Example: six SQL `ORDER BY` / `GROUP BY` paths had drifted to `Castro - Part 2`, showing a partition for a shed that has none.

---

## Worked Examples: The Failures Fixed

### Example 1: Name-Keying Merges Parks

**Bug:** A UI selector grouped animals by shed name alone:

```kotlin
// WRONG: Grouped by name, silent park merge
animalsBySheds = animals.groupBy { it.shedName }  // "Castro" collapses both parks

// CORRECT: Group by shed_id (UUID) + park
animalsBySheds = animals.groupBy { it.parkId to it.shedId }
```

Two parks each have a `Castro` shed (different `shed_id`). Grouping by name collapses them onto one.

**Impact:** Six duplicate `Godel 1` rows appeared in a shed selector because six partitions of the same physical shed got grouped as one "Godel 1" row instead of six disjoint rows.

---

### Example 2: Naive Join Produces Invalid Names

**Bug:** Combining shed name and partition with a space:

```sql
-- WRONG: `Godel 1` (shed) + `Part 3` (partition) = `Godel 1 Part 3` (renders as `Godel 1 1` when truncated)
SELECT shed_name || ' ' || partition_label AS location_display
FROM locations
WHERE shed_name = 'Godel 1' AND partition_label = 'Part 3'
-- Output: `Godel 1 Part 3` — loses the dashes and word spacing that distinguish label from shed

-- CORRECT: Use the canonical display function or explicit separator
SELECT DisplayName(shed_name, partition_label) AS location_display
-- Output: `Godel 1 - Part 3` — matches master registry
```

**Impact:** The maintainer photographed a weighing screen showing `Godel 1 1` — the shed name `Godel 1` followed by a truncated partition number. The naive join had produced the label; text truncation did the rest.

---

### Example 3: Missing Partition Columns in New Tables

**Bug:** Three new tables shipped without `partition_label`, making them impossible to join to the correct physical location:

- `verification_items` — verifier proof videos (missing partition, cannot tell which of 10 `Godel 1` partitions the evidence applies to)
- `weighing_campaign_sheds` — weighing task assignments (missing partition, assigns to `Godel 1` ambiguously)
- `health_cases` — clinical incidents (missing partition, cannot find the right clinical record)

**Fix:** Add `partition_label` column + a backend-composed `operational_location_display` to every location-bearing response struct.

---

### Example 4: Query Drift (SQL Copy-Paste)

**Bug:** Six SQL paths each independently composed the display name, and they drifted:

```sql
-- Path 1 (canonical, from Go helper reference):
'Godel 1 - Part 3'

-- Path 2 (legacy query, copy-pasted, never updated):
'Godel 1-Part 3'   -- missing spaces

-- Path 3 (another hand-roll):
'GODEL 1 - PART 3' -- uppercase (matches BigQuery, not registry)

-- Path 4 (from a read model):
'Godel 1 Part 3'   -- space instead of dash (the naive join)

-- Paths 5–6: Similar drifts in projection queries
```

**Impact:** The same animal's location rendered differently on each screen, breaking filtering and grouping in reports.

---

## Storage vs. Product Semantics

This is the maintainer's critical insight (2026-08-05):

| Layer | Meaning | Example |
|-------|---------|---------|
| **Storage** (backend database) | Normalization for querying efficiency | `shed_name='Castro'` + `partition_label='2'` |
| **Product** (user-facing surfaces) | ALWAYS show both when partition exists | `Castro 2` (for numeric suffix) or `Godel 1 - Part 3` (for prefixed) |

Both halves must always be read together when working on location-bearing features.

---

## Complete Rule Set

### Mandatory for All Locations

1. **Normalize at seed/import.** Raw partition labels must be parsed and normalized into `(shed_id, shed_name, partition_label)` BEFORE writing to `locations`. Do not invent new rows for undivided sheds.

2. **Carry both halves in responses.** Every location-bearing response struct must include:
   - `shed_id` (UUID, the canonical key)
   - `shed_name` (text, the display name of the physical shed)
   - `partition_label` (text or NULL, the partition within that shed)
   - `operational_location_display` (text, composed by the backend: `DisplayName(shed_name, partition_label)`)

3. **Query by `shed_id` + park, not by name.** When grouping, filtering, or scoping to a location, use UUID + park, never shed name alone.

4. **Never omit the partition in product display.** If `partition_label` is not NULL, the display must show it. If it IS NULL, render the shed name alone.

5. **Use canonical composition.** Render location displays only via the shared helpers. Do not hand-roll the display string.

6. **Add `partition_label` to new location-bearing tables.** Any table that records location (vaccination completions, proof assignments, health cases, shifting source/destination, etc.) must have a `partition_label` column (nullable for undivided sheds).

### Mandatory for Subagent Briefs

If delegating location-bearing work to a subagent:

- **Include the rule in the brief.** Quote or link to this ADR and the worked examples.
- **Name what makes the work location-bearing.** ("This task updates shed-scoped queries" or "This PR adds a new location picker".)
- **Specify the expected input and output shapes.** If the brief doesn't name the boundary, the agent will cross it reasonably and it will be wrong.

---

## Machine Enforcement

**Guard:** `make operational-location-guard` (in `make guardrails` and `make ci-local`)

The guard runs three checks:

1. **Name-keying validator** — detects GROUP BY / SELECT DISTINCT on `locations.name` without `park_id` or `shed_id` co-grouping.
2. **Display drift detector** — scans all location-display assignments and checks them against the canonical helper output for known seeds.
3. **Missing partition columns** — flags new rows in location-bearing tables that lack a `partition_label` field.

### Known Blind Spots

1. **Hardcoded literal strings** — a query with a hardcoded `'Castro 1'` literal will not be caught; use Grep to audit those.
2. **Dynamic composition in application code** — concatenation in Go/Kotlin/TypeScript string templates may not be caught; run the display helpers through unit tests for all known locations.
3. **Reflective queries** — queries built via string concatenation in middleware or ORM are not analyzed statically.
4. **Commentary only** — a comment explaining the rule without enforcing it counts as a comment, not an enforcement.

**For these cases, rely on code review.** The guard catches the structural class; the brief and code review catch the semantic class.

---

## Related Docs

- `AGENTS.md` → "Operational Location and Partition Convention" (maintainer lock)
- `context/repo-audits/operational-location-do-not-reopen-ledger.md` — ledger of all defects found 2026-08-06
- `docs/features/weighing/TRD.md` → shed selection (weighing context)
- `docs/features/vaccination/TRD.md` → drive planning (vaccination context)

---

## Decision Timeline

- **2026-07-19:** Maintainer noted that movement is within-park only; parks and sheds are distinct.
- **2026-08-03:** Weighing screens showed `Godel 1 1` (truncated partition label + shed name).
- **2026-08-04:** Herd register showed multiple `Godel 1` rows instead of six distinct partitions.
- **2026-08-05:** Evidence gathering from master registry, BigQuery, and legacy code confirmed three independent sources use dashed form.
- **2026-08-06:** Full convention codified. Guard added. This ADR written.

This decision is FINAL and LOCKED. Any future proposal to relax the partition requirement, allow name-keying, or omit the partition from product display MUST start by explaining why the worked examples no longer apply.
