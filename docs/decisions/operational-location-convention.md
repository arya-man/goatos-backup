# Operational Location Convention: Park + Exact Shed

**Status:** Accepted (2026-08-06)  
**Guard:** `make operational-location-guard`  
**Owned by:** Maintainer (STANDING LOCK)

**Verification Checklist:** See AGENTS.md → "Partition Change Verification Checklist" (mandatory before committing any location change).

**The 10 Defect Classes Encoded:** See AGENTS.md → "Ten Defect Classes From Session 2026-08-07" (the lessons that make recurrence impossible).

---

## Problem Statement

Goat OS tracks animals at the exact physical shed where they live. Some sheds have a shared/base group name for humans, but the live/operator location is still the exact shed row, not a parent plus a partition. Early implementations diverged on how to name, store, display, and query these locations:

- Some code wrongly normalized `Castro 1` and `Castro 2` to a single shed `Castro` with partitions, while the farm reality is separate physical sheds `Castro 1` and `Castro 2`.
- Name-based grouping silently merged animals across parks (two `Castro` sheds exist — one in Mandela, one in Godel).
- Historical data sources used multiple spellings (`Godel 1 Part 3`, `GODEL 1 - PART 4`, `Godel 1 1`). The live catalog must converge those to the single exact shed string chosen for Goat OS, e.g. `Godel 1 Part 3`.
- The term "active shed" was used to mean both "a physical building" and "a building currently holding animals", blurring storage and product semantics.
- Six query paths had already drifted from the canonical composition rule, producing bugs like `Godel 1 1` (naive space-numeric join) and duplicate `Godel 1` rows in UI selectors.

**Evidence gathered 2026-08-06 from three independent sources:**

| Source | Format | Example | Scale |
|--------|--------|---------|-------|
| **Master Registry / Aryaman list** | Text rows | `Castro 1`, `Gandhi 1`, `Godel 1 Part 1`, `Mandela 2 Part 1` | One row per physical shed |
| **Legacy/seed data** | Parent/group plus label bridge | `shed_group=Godel 1`, `partition_label=Part 3` | Compatibility only; must resolve to exact shed before operator use |
| **Legacy production code** | Hardcoded constants in feed automation + dashboard UI | older strings such as `Mandela 2 Part 3` | Historical evidence, not the live naming contract |

---

## Convention: Unified Definition

The ground location of any goat is a two-part identity:

```
OperationalLocation = park + exact_shed
```

**Three rules form the complete convention:**

### Rule 1: Storage Normalization (Backend)

The `locations` table stores one row per physical shed. The physical shed name is a single string.

- `Castro 1`, `Castro 2`, `Castro 3` are three shed rows.
- `Gandhi 1`, `Gandhi 2`, `Gandhi 3` are three shed rows.
- `Godel 1 Part 3` is one shed row.
- `Mandela 2 Part 1` is one shed row.

When old rows still have group/partition columns, those columns are a bridge only:

- `shed_group_id` = optional parent/group header.
- `partition_label` = historical matching key.
- `goats.shed_id` and task/bucket location ids = exact physical shed.

No live query should join `shed_name + partition_label` to invent a display name.

### Rule 2: Product Display (All Surfaces)

User-facing surfaces always show the exact shed string:

- Plain shed: `Yashoda`, `Ho Chi Minh`
- Numbered shed: `Castro 1`, `Castro 2`, `Gandhi 3`
- Part-named shed: `Godel 1 Part 3`, `Mandela 2 Part 1`

**NEVER render:**
- `Yashoda whole` — `'whole'` is a matching key for queries, never copy for users
- `Godel 1 1` — result of naive space-numeric join (worked example of what breaks)
- Exact shed name plus compatibility label again, e.g. `Castro 2 2`, `Gandhi 1 1`, `Godel 1 Part 1 Part 1`

Rendering uses the exact shed display from the backend. `partition_label` may be present for old matching logic but must not change visible text.

### Rule 3: Composition Location (Code)

Shared location-composition logic lives in ONE place per language — use it instead of hand-rolling:

- **Go:** `backend/internal/platform/oploc` — provides `DisplayName(shed, partition)`, querying helpers, and constants
- **Admin-web:** `apps/admin-web/lib/operational-location.ts` — TypeScript helpers for display and filtering
- **Android:** `core/core-ui/.../PartitionLabel.kt` — Kotlin composable for rendering the partition label and full location

Hand-rolled copies drift. Example: six SQL `ORDER BY` / `GROUP BY` paths had drifted to `Castro 2`, showing a partition for a shed that has none.

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

**Bug:** Combining a group shed name and a compatibility partition label:

```sql
-- WRONG: `Godel 1` (group) + `Part 3` (compatibility label)
SELECT shed_name || ' ' || partition_label AS location_display
FROM locations
WHERE shed_name = 'Godel 1' AND partition_label = 'Part 3'
-- Output happens to look like `Godel 1 Part 3`, but the identity is still wrong.

-- CORRECT: read the exact shed row directly.
SELECT exact_shed.name AS location_display
FROM locations exact_shed
WHERE exact_shed.name = 'Godel 1 Part 3'
```

**Impact:** The maintainer photographed screens showing `Castro 2 2` and `Gandhi 1 1`: the exact shed name already contained the number, and old code appended the compatibility label again.

---

### Example 3: Missing Partition Columns in New Tables

**Bug:** Three new tables shipped without the exact operational shed id, making them impossible to join to the correct physical location:

- `verification_items` — verifier proof videos (must point at `Godel 1 Part 3`, not hidden group `Godel 1`)
- `weighing_campaign_sheds` — weighing task assignments (must point at the exact shed row)
- `health_cases` — clinical incidents (must carry the exact current shed row)

**Fix:** Store/read the exact shed id everywhere. Compatibility fields such as `partition_label` may exist only to migrate old rows or keep stale mobile cache separated during rollout.

---

### Example 4: Query Drift (SQL Copy-Paste)

**Bug:** Six SQL paths each independently composed the display name, and they drifted:

```sql
-- Path 1 (canonical exact shed row):
'Godel 1 Part 3'

-- Path 2 (legacy query, copy-pasted, never updated):
'Godel 1-Part 3'   -- missing spaces

-- Path 3 (another hand-roll):
'GODEL 1 - PART 3' -- uppercase (matches BigQuery, not registry)

-- Path 4 (from a read model):
'Godel 1 Part 3'   -- visually right, but still wrong if it was hand-composed from group + label

-- Paths 5–6: Similar drifts in projection queries
```

**Impact:** The same animal's location rendered differently on each screen, breaking filtering and grouping in reports.

---

## Storage vs. Product Semantics

This is the maintainer's critical insight (2026-08-05):

| Layer | Meaning | Example |
|-------|---------|---------|
| **Storage** (backend database) | The animal/work row points at the exact physical shed row | `goats.shed_id = locations('Castro 2')` |
| **Compatibility** (old imports/cache/history) | Optional bridge fields that must not drive live identity/display | `shed_group_id='Castro'`, `partition_label='2'` |
| **Product** (user-facing surfaces) | Show the exact shed row name as-is | `Castro 2` or `Godel 1 Part 3` |

The exact shed id is the live identity. Compatibility partition labels must not be appended to it.

---

## Complete Rule Set

### Mandatory for All Locations

1. **Normalize at seed/import.** Raw partition labels must be resolved into exact shed rows BEFORE writing live animal/work locations. Do not leave live rows pointing at a group shed plus partition label.

2. **Carry both halves in responses.** Every location-bearing response struct must include:
   - `shed_id` (UUID of the exact physical shed)
   - `shed_name` (text, the exact physical shed name)
   - `operational_location_display` (normally the same exact shed name)
   - optional compatibility metadata such as `shed_group_id` or `partition_label` only when needed for legacy cache/history

3. **Query by `shed_id` + park, not by name.** When grouping, filtering, or scoping to a location, use UUID + park, never shed name alone.

4. **Never append compatibility partition labels in product display.** Render the exact shed name alone.

5. **Use canonical exact-shed helpers.** Render location displays only via shared helpers that prefer exact shed names and strip stale compatibility labels. Do not hand-roll display strings.

6. **New location-bearing tables must store exact shed ids.** Any table that records location (vaccination completions, proof assignments, health cases, shifting source/destination, etc.) must point at the exact operational shed row. Add compatibility columns only for migration boundaries, not live identity.

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
1. Inserts a known partitioned shed into the test database (e.g., `Godel 1 Part 3`)
2. Calls the API/query/screen that READS that shed
3. Asserts the RETURNED STRING exactly matches the database round-trip (e.g., `operational_location_display = 'Godel 1 Part 3'`)

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

`Yashoda 2` is a SHED NAME, whole. It renders `Yashoda 2`, never `Yashoda 2`. Same for `Ho Chi Minh 1`. The trailing number is part of the name, not a partition.

**Contrast:** A genuinely partitioned shed renders as `Mandela 1 - Part 2` (shed `Mandela 1` + partition `Part 2`).

**Defect:** A test fixture fed `operationalLocationLabel("Yashoda", "2")` and its expectation was "corrected" to `Yashoda 2`. The formatter was right; the FIXTURE was wrong. **Rule: a fixture that asserts a shape the farm does not have is a defect even when the assertion passes.**

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
- **2026-08-06:** Full convention codified. Guard added. This ADR written. Do-not-reopen ledger created.
- **2026-08-07:** Session found 15 defects (OL-1..OL-15) across the ~5-handoff chain. Updated AGENTS.md with 10 defect classes and partition-change verification checklist. Added guard check 5 (snapshot staleness). Extended do-not-reopen ledger to include OL-10..OL-15.

This decision is FINAL and LOCKED. Any future proposal to relax the partition requirement, allow name-keying, or omit the partition from product display MUST start by explaining why the worked examples no longer apply.
