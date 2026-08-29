# Operational Location Convention: Park + Shed + Partition

**Status:** Accepted (2026-08-06)  
**Guard:** `make operational-location-guard`  
**Owned by:** Maintainer (STANDING LOCK)

**Verification Checklist:** See AGENTS.md → "Partition Change Verification Checklist" (mandatory before committing any location change).

**The 10 Defect Classes Encoded:** See AGENTS.md → "Ten Defect Classes From Session 2026-08-07" (the lessons that make recurrence impossible).

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

- No partition (NULL / '' / 'whole') → Display the plain shed name: `Yashoda`, `Ho Chi Minh 1`
- Bare numeric partition → Space separator: `Castro 1`, `Gandhi 2`, `Castro 3` (the farm's physical shed names, as painted on buildings)
- Worded partition → Dash separator: `Godel 1 - Part 3`, `Mandela 1 - Part 1`, `Mandela 2 - Part 10`

**Separator rule (maintainer decision 2026-08-16, clarifying farm's real-world naming):**
- Bare numerals use SPACE because `Castro 1`, `Gandhi 2`, etc. ARE the real names painted on the farm's sheds — not a display formatting choice.
- Worded labels use " - " (space-dash-space) for visual boundary: `Godel 1 - Part 3` is unambiguous. Without the dash, `Godel 1` + `Part 3` = `Godel 1 Part 3` loses semantic clarity.
- On live STG data 98 of 130 destination options (75%) have digit-terminated shed names. The space form for numerics stays unambiguous ONLY because the worded convention always uses " - ", signaling to the reader "what follows is a partition label, not part of the shed name."

**NEVER render:**
- `Yashoda whole` — `'whole'` is a matching key for queries, never copy for users
- `Godel 1 1` — result of naive space-numeric join (worked example of what breaks)
- `Castro - 1` — dash form for numeric partitions (contradicts the farm's physical naming)
- Shed name alone when a partition exists (e.g., `Godel 1` without the partition) — **always carry both halves**

**Both halves must always be read together.** Rendering the display requires BOTH `shed_id` (and its display name) AND `partition_label` in the response struct.

### Weighing Read Models: Partition Composition

Weighing read models must treat every row as an operational location, never as a loose shed name. Composition chips/brackets on the Weights page are keyed by `(location_id, partition_label)` and may need to resolve two historical data shapes:

- Worded partition buckets: `location_id = Godel 2`, `partition_label = Part 1`. The cohort lookup must read only goats whose `goat_shed_partitions.shed_id` is `Godel 2` and whose partition is `Part 1`.
- Numeric display rows: `location_id = Castro 1`, `partition_label = ''`. If no goats live directly on `Castro 1`, the lookup may resolve to physical shed `Castro` partition `1`, but only when a matching `goat_shed_partitions` row exists. The same rule applies to `Castro 2/3`, `Gandhi 1/2/3`, and legacy `Gandi 1/2/3`.

Do not infer partitions from every trailing number. A real standalone shed such as `Ho Chi Minh 1` or `Plain 1` stays an undivided shed unless the partition table proves otherwise.

Guardrail: `make weighing-partition-composition-guard` runs `TestWeightDemographicsLumpCompositionResolvesPhysicalShedPartitions`, covering `Godel 2 - Part 1`, `Castro 1/2/3`, `Gandhi 1/2/3`, legacy `Gandi 1/2/3`, and a numeric non-partition shed.

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

This is the maintainer's critical insight (2026-08-05, clarified 2026-08-16):

| Layer | Meaning | Example |
|-------|---------|---------|
| **Storage** (backend database) | Normalization for querying efficiency | `shed_name='Castro'` + `partition_label='2'` |
| **Product** (user-facing surfaces) | ALWAYS show both when partition exists | `Castro 2` (numeric: space, farm's real name) or `Godel 1 - Part 3` (worded: dash, visual boundary) |

Both halves must always be read together when working on location-bearing features. The numeric form `Castro 2` is not a formatting choice — it reproduces the actual shed name an operator sees and uses every day.

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

## Validation Strategy (Session 2026-08-07)

The partition convention spans ~5 handoffs (SQL → Go → wire DTO → OpenAPI → client render). Missing or corrupt values at ANY ONE point render a bare shed name end-to-end. Two validation patterns prevent this:

### Cross-Layer Proof: DB Round-Trip Test

Every location-displaying change MUST include an integration test that:
1. Inserts a known partitioned shed into the test database (e.g., `Godel 1 - Part 3`)
2. Calls the API/query/screen that READS that shed
3. Asserts the RETURNED STRING exactly matches the database round-trip (e.g., `operational_location_display = 'Godel 1 - Part 3'`)

Unit tests on Go formatters alone are insufficient (OL-9). Pure OpenAPI schema checks alone are insufficient (OL-10). The proof must span all five layers in one assertion.

### Structural Checks: Schema + Struct Parity

When a location response field is added to OpenAPI:
1. Verify the Go struct has the same field (same name, same type)
2. Verify the wire emitter populates the struct (scan the constructor/adapter)
3. Verify the test asserts the populated value end-to-end

Mismatch at any step (schema declared, struct missing) is a release blocker (OL-10, OL-11).

### Subclass Check: Name-Keying Audit

When grouping, filtering, or counting by location:
1. Grep for `groupBy`, `GROUP BY`, `DISTINCT`, and any `WHERE shed_name = ` patterns
2. Verify the key includes `shed_id` (UUID) + `park_id`
3. Never key by `shed_name` alone (names repeat across parks — OL-15, OL-2)
4. Run `make operational-location-guard` which flags this class

---

## Machine Enforcement

**Guard:** `make operational-location-guard` (in `make guardrails` and `make ci-local`)  
**Executable:** `tools/agent-hooks/check-operational-location.mjs`  
**Registration:** `tools/ci/guardrail-manifest.json`

The guard runs five checks (plus the three blind-spot cases below):

1. **Name-keying validator** — detects GROUP BY / SELECT DISTINCT on `locations.name` without `park_id` or `shed_id` co-grouping. (OL-2, OL-15)
2. **Response schema validator** — checks OpenAPI response schemas (not just Request) for location fields; ensures `partition_label` and `operational_location_display` are declared when location is present. (OL-10, OL-11)
3. **Display drift detector** — scans all location-display assignments and checks them against the canonical helper output for known seeds. (OL-3, OL-7)
4. **Missing partition columns** — flags new rows in location-bearing tables that lack a `partition_label` field. (OL-4, OL-5, OL-6)
5. **Snapshot staleness** — flags `*assignment` tables that read `partition_label` directly instead of resolving from catalog. (OL-13)

**Configuration note:** The guard requires `tools/ci/guardrail-manifest.json` to list `operational-location-guard` as part of the mandatory `run_common` suite. If the manifest does not include it, the guard does not run in `make ci-local` and will not block breaks.

### Known Blind Spots

1. **Hardcoded literal strings** — a query with a hardcoded `'Castro 1'` literal will not be caught; use Grep to audit those.
2. **Dynamic composition in application code** — concatenation in Go/Kotlin/TypeScript string templates may not be caught; run the display helpers through unit tests for all known locations.
3. **Reflective queries** — queries built via string concatenation in middleware or ORM are not analyzed statically.
4. **Struct fields that go unpopulated** — if a Go struct field is declared but never written, only E2E/DB-round-trip tests will catch it (OL-3).
5. **OpenAPI field mismatches with Go structs** — the guard checks field presence in schemas; field-by-field parity with the Go type requires manual verification (OL-10, OL-11).
6. **Commentary only** — a comment explaining the rule without enforcing it counts as a comment, not an enforcement.

**For these cases, rely on code review and the DB-round-trip test.** The guard catches structural classes; the verification checklist (AGENTS.md) and tests catch semantic gaps.

---

## Related Docs

- `AGENTS.md` → "Operational Location and Partition Convention" (maintainer lock, full context)
- `AGENTS.md` → "Ten Defect Classes From Session 2026-08-07" (the 10 lessons that prevent recurrence)
- `AGENTS.md` → "Partition Change Verification Checklist" (mandatory before any commit)
- `context/repo-audits/operational-location-do-not-reopen-ledger.md` — all defects OL-1..OL-15, status, and closure criteria
- `.agents/skills/frontend-anti-patterns/SKILL.md` — partition display rule for admin-web
- `.agents/skills/mobile-anti-patterns/SKILL.md` — partition display rule for Android
- `.agents/skills/db-migration-safety/SKILL.md` — location-bearing schema requirements
- `.agents/skills/goatos-code-review/SKILL.md` → references/kernel-and-scale.md — full review context
- `docs/features/weighing/TRD.md` → shed selection (weighing context)
- `docs/features/vaccination/TRD.md` → drive planning (vaccination context)

---

## Five Essential Rules From Session 2026-08-07 (Hard Encoding)

These rules emerged from defects discovered on production code and live data TODAY. Each one makes a class of defect impossible. Encode them in every subagent brief and code review.

### Rule 1: An Undivided Shed Whose Name Ends in a Number Is Never Split

`Yashoda 2` is a SHED NAME, whole. It renders `Yashoda 2`, never `Yashoda - 2`. Same for `Ho Chi Minh 1`. The trailing number is part of the name, not a partition.

**Contrast:** A genuinely partitioned shed renders as `Mandela 1 - Part 2` (shed `Mandela 1` + partition `Part 2`).

**Defect:** A test fixture fed `operationalLocationLabel("Yashoda", "2")` and its expectation was "corrected" to `Yashoda - 2`. The formatter was right; the FIXTURE was wrong. **Rule: a fixture that asserts a shape the farm does not have is a defect even when the assertion passes.**

### Rule 2: A Required Contract Field Must Be Populated on Every Construction Path, in the Same Change

Marking a field `required` in OpenAPI while the Go struct lacks it, or has it but never fills it, ships a contract the client cannot rely on. This happened EIGHT times on this branch.

**The complete checklist:** SQL column → scan destination → Go struct field → populated at EVERY construction site → wire DTO → OpenAPI → generated client → renderer. A gap at ANY hop renders bare shed name end to end.

**Defect:** `operational_location_display` was required on `WeighingShedVideos` in OpenAPI while the serving struct had neither field nor composition logic.

### Rule 3: Scaffolded Is Not Wired

A migration, domain field, decoder helper, OpenAPI entry, and two client DTOs can all exist while the repository and handler touch none of them. Field-presence tests and pure-formatter unit tests both pass for scaffolding.

**Rule: a feature is not done until a test asserts the OUTPUT STRING on a real round trip.**

**Defect:** The partition feature added SQL migration, domain field, decoder helper, OpenAPI schema, and client DTOs — yet the handler never called the decoder and never populated the field. Real output was bare shed name.

### Rule 4: Verify Data Against the Live Database Before Writing a Repair

~500 lines of guarded repair SQL were written against a mistaken reading of STG inferred from code. A single read-only check showed every repair class returns ZERO rows.

**Rule: query the live database FIRST; a repair script written from inferred shape is a destructive operation aimed at a problem that may not exist.**

**Defect:** Repair scripts were generated to handle hypothetical `Mandela 1` partition-catalog orphans that never existed in STG.

### Rule 5: Confirm the Repo Path Before Editing

This workspace has multiple checkouts (`<another checkout>`, `<this repo>`, review worktrees). An agent who does not confirm its tree will edit the wrong one.

**Rule for delegated work:** state the absolute repo path in the brief and confirm with `git rev-parse --show-toplevel` before the first edit.

**Defect:** Subagent edited `<another checkout>/docs/decisions/...` instead of `<this repo>/docs/decisions/...` and reported files "don't exist" when simply in the wrong tree.

---

## Decision Timeline

- **2026-07-19:** Maintainer noted that movement is within-park only; parks and sheds are distinct.
- **2026-08-03:** Weighing screens showed `Godel 1 1` (truncated partition label + shed name).
- **2026-08-04:** Herd register showed multiple `Godel 1` rows instead of six distinct partitions.
- **2026-08-05:** Evidence gathering from master registry, BigQuery, and legacy code confirmed three independent sources use dashed form.
- **2026-08-06:** Full convention codified (initial version). Guard added. This ADR written. Do-not-reopen ledger created.
- **2026-08-07:** Session found 15 defects (OL-1..OL-15) across the ~5-handoff chain. Updated AGENTS.md with 10 defect classes and partition-change verification checklist. Added guard check 5 (snapshot staleness). Extended do-not-reopen ledger to include OL-10..OL-15.
- **2026-08-16:** **SUPERSESSION** — Maintainer clarification: numeric partitions display with SPACE (farm's real physical naming, e.g., "Castro 1" as painted on buildings), not dashed form. Prefixed/worded partitions use dash for visual boundary (e.g., "Godel 1 - Part 3"). Updated Rule 2, storage-vs-product table, worked examples, and all docstrings across Go/TypeScript/Kotlin to reflect this clarification. The separator rule distinguishes two naming conventions at the farm level, not an arbitrary formatting choice.

This decision is FINAL and LOCKED. Any future proposal to relax the partition requirement, allow name-keying, or omit the partition from product display MUST start by explaining why the worked examples no longer apply.
