# Operational Location Convention Defects (Do-Not-Reopen Ledger)

**Date:** 2026-08-06  
**Status:** CLOSED / DO-NOT-REOPEN  
**Guard:** `make operational-location-guard`  
**Decision:** `docs/decisions/operational-location-convention.md`

---

## Incident Summary

Session 2026-08-06 audited location naming, storage, and display across the Goat OS codebase. The maintainer required that future implementations make the partition convention IMPOSSIBLE to get wrong. This ledger records every defect found, its fix, and the evidence that established the ground truth.

---

## Defect Registry

### OL-1: Name-Keying Silent Park Merge (VERIFIED HIGH IMPACT)

**Found:** Herd register UI selector grouped animals by shed name alone  
**Defect:** `groupBy { it.shedName }` — two parks each have a `Castro` shed (different `shed_id`); name-only grouping collapses them  
**Impact:** 324 CPT adults + 89 Mandela adults both assigned to one `Castro` selector row; operator-selection was ambiguous  
**Fix:** Change to `groupBy { it.parkId to it.shedId }`  
**Status:** VERIFIED — evidence in CRG and live CBE/CPT data  

---

### OL-2: Duplicate Partition Rows in UI (VERIFIED HIGH IMPACT)

**Found:** Shed selector showed six rows of `Godel 1` instead of six distinct partitions  
**Defect:** Six partitions (`Part 1` … `Part 10`, only 6 are populated) of the same physical shed `Godel 1` were grouped as one "Godel 1" entry because the grouping key was shed name, not `(shed_id, partition_label)`  
**Impact:** Operator could not distinguish which `Godel 1` partition to assign work to; selections were random  
**Fix:** Add `partition_label` to the group key; use canonical display helper for rendering  
**Status:** VERIFIED — photographed by maintainer 2026-08-06  

---

### OL-3: Naive Join Produces Invalid Display Names (VERIFIED CRITICAL)

**Found:** Weighing screens rendered `Godel 1 1` (truncated, invalid format)  
**Defect:** Naive string concatenation `shed_name || ' ' || partition_label` on `('Godel 1', 'Part 3')` produces `'Godel 1 Part 3'`, which when truncated at 8 chars becomes `'Godel 1 '` or worse, `'Godel 1 1'` (if the last token is re-rendered from a digit)  
**Root Cause:** Operator code did not use the canonical `DisplayName(shed, partition)` helper; instead hand-rolled the composition  
**Impact:** Weighing task board was unreadable; operators could not identify which partition they were weighing  
**Fix:** Use canonical helpers (`backend/internal/platform/oploc.DisplayName`) for ALL location display strings  
**Status:** VERIFIED — maintainer photographed this screen 2026-08-03  

---

### OL-4: Missing `partition_label` Column on `verification_items` (FOUND, UNEXAMINED)

**Found:** Verification proof assignments lack a partition column  
**Defect:** `verification_items` records proof videos but does not store `partition_label`. When a verifier reviews a video of animals in `Godel 1 - Part 3`, the record does not say which partition the animals are in, making it impossible to:
  - Filter proofs by partition in verifier queue
  - Correlate proof videos to the specific partition-scoped work request
  - Report proof review metrics per partition
**Impact:** Verifier cannot distinguish which `Godel 1` partition a proof applies to; metrics and filtering are aggregated at shed-only granularity  
**Fix:** Add `partition_label` (nullable) column + compose `operational_location_display` in API responses  
**Status:** NOT FIXED — blocker for location-aware verification  

---

### OL-5: Missing `partition_label` Column on `weighing_campaign_sheds` (FOUND, UNEXAMINED)

**Found:** Weighing task assignments lack a partition column  
**Defect:** `weighing_campaign_sheds` assigns weighing work to sheds but does not store `partition_label`. Weighing tasks are always assigned to a physical location (partition if subdivided, shed name if not), and the table must record both.  
**Impact:** Weighing tasks assigned to `Godel 1` do not specify which partition(s) the operator should weigh; the operator must infer from context or ask  
**Fix:** Add `partition_label` (nullable) column + update the task-creation API and UI to capture and store it  
**Status:** NOT FIXED — blocker for partition-aware weighing assignment  

---

### OL-6: Missing `partition_label` Column on `health_cases` (FOUND, UNEXAMINED)

**Found:** Clinical incidents lack a partition column  
**Defect:** `health_cases` records clinical incidents (quarantine, disease, observation) for animals but does not store the animal's `partition_label` at the time of the incident. This breaks the ability to:
  - Audit clinical history by partition (e.g., "was there a disease cluster in `Godel 1 - Part 4` in July?")
  - Correlate clinical incidents across animals in the same partition
  - Report clinical metrics per partition/shed  
**Impact:** Clinical analysis is shed-level only; partition-scoped epidemiology is impossible  
**Fix:** Add `partition_label` (nullable) column to `health_cases`; ensure clinical captures carry partition context  
**Status:** NOT FIXED — blocker for partition-aware health reporting  

---

### OL-7: Query Drift in SQL Display Composition (FOUND, HIGH IMPACT)

**Found:** Six SQL query paths each independently composed location display names, resulting in divergent outputs  
**Defect Instances:**

| Query Path | Format | Notes |
|------------|--------|-------|
| Canonical (Go helper ref) | `Godel 1 - Part 3` | Source of truth |
| Legacy feed automation | `Godel 1-Part 3` | Missing spaces |
| Dashboard counting view | `GODEL 1 - PART 3` | Uppercase; matches BigQuery, not registry |
| Read model projection | `Godel 1 Part 3` | Space instead of dash (naive join) |
| Herd register | `Godel 1 Part 3` | Same as naive join |
| Weighing detail fetch | `Godel 1 - Part 3` | Correct (manual check) |

**Impact:** The same animal's location rendered differently on each screen (Counting view vs. Herd Register vs. Weighing), breaking filtering, sorting, and cross-screen navigation  
**Fix:** Audit all SQL ORDER BY / GROUP BY / SELECT DISTINCT on locations; replace hand-rolled compositions with canonical helpers or materialized `operational_location_display` column from the backend  
**Status:** IDENTIFIED — six paths flagged; requires per-query review and fix  

---

### OL-8: Code Confusion Between Shed Name Suffix and Partition Number (FOUND, DESIGN)

**Found:** Shed names like `Godel 1`, `Mandela 2`, `Ho Chi Minh 1` are difficult to distinguish from partitions  
**Root Cause:** Legacy naming convention uses numeric suffixes on shed names for historical reasons (e.g., "Godel 1 is the main Godel shed, Godel 2 is an expansion"). But partition labels are ALSO numeric (e.g., `Part 1`, `Part 2`), creating visual ambiguity.  
**Example:** A naive parser might read `Godel 1 Part 3` as "shed Godel, partition 1, part 3" instead of "shed Godel 1, partition Part 3".  
**Impact:** Developers re-reading code sometimes misidentify which part of the tuple is the shed vs. partition; documentation must be very explicit  
**Mitigation:** 
  - Always use `shed_id` (UUID) as the primary key, never shed name
  - Always pair shed name with partition label; never render one without the other when both exist
  - Use canonical display helpers that handle this unambiguity
  - Add comments wherever manual composition happens
**Status:** DESIGN ISSUE — not a bug, but a design debt that makes bugs more likely  

---

### OL-9: No Real `shed_partitions` Catalog (DESIGN BLOCKER)

**Found:** The `locations` table is the only source of truth for which partitions exist  
**Defect:** Legacy locations table stores both active and inactive sheds/partitions in one denormalized row. A proper catalog table `shed_partitions` (keyed by `(tenant_id, shed_id, normalized_label)`) exists as a schema in AGENTS.md but not in the actual database.  
**Impact:**
  - New location-bearing tables cannot reference a canonical `shed_partitions` FK
  - Partition enumerations must always join to `locations` (which is large and slow)
  - Empty partitions are invisible (a partition with zero animals does not appear in goat-scoped queries)
  - `weighing_campaign_sheds` cannot validate that an assigned partition actually exists
**Workaround:** Until `shed_partitions` is built, use `locations` as the partition catalog and accept that active/inactive mixing is a legacy state. Migrate manually when the real table ships.  
**Status:** DESIGN DEBT — documented in AGENTS.md; requires future migration  

---

## Evidence and Sources

| Source | Scope | Confidence | Date |
|--------|-------|-----------|------|
| **Master Registry** (`Sheds DB.xlsx` via Mesha wiki graph) | `Godel 1 - Part 1` … `Godel 1 - Part 10` (10 partitions) | HIGH | 2026-08-06 |
| **Live BigQuery** (`goatos-sheets.counting.counting_db_view`) | `MANDELA 1 - PART 3` (639 animals), `GODEL 1 - PART 4` (450), ~100:1 dashed form usage | HIGH | 2026-08-06 |
| **Live BigQuery** (`Shiftings.shiftings_fact`) | `Godel 2 - Part 1` (145 animals), confirms dashed form in prod data | HIGH | 2026-08-06 |
| **Legacy Production Code** (`slack-automation-scripts/feed_automation.js`) | Hardcoded `'Mandela 2 - Part 3'` | MEDIUM (one data point) | Before 2026-08-06 |
| **Legacy UI** (`dashboard/lib/constants.ts`, `SHED_CAPACITIES`) | `"Gandhi 1 - Part 1"`, `"Castro 1"` (both numeric and dashed names) | MEDIUM | Before 2026-08-06 |
| **Operator-Entered Data** (counting CSVs from live operations) | Both `Castro 1` and `Castro - Part 1` formats observed | MEDIUM (operator variation) | 2026-06–2026-08 |

---

## Closure Criteria

All of the following must be true before closing defects OL-4, OL-5, OL-6:

1. ✗ (Pending) Schema migration adds `partition_label` to `verification_items`
2. ✗ (Pending) Schema migration adds `partition_label` to `weighing_campaign_sheds`
3. ✗ (Pending) Schema migration adds `partition_label` to `health_cases`
4. ✗ (Pending) Verification API response includes composed `operational_location_display`
5. ✗ (Pending) Weighing API response includes composed `operational_location_display`
6. ✗ (Pending) Health API response includes composed `operational_location_display`
7. ✗ (Pending) All six SQL display composition paths audited and corrected
8. ✓ (Done) Guard `make operational-location-guard` runs in CI and catches future regressions

OL-7 (query drift) remains OPEN until all six paths are verified. OL-8 (design confusion) and OL-9 (catalog blocker) are DESIGN DEBT, not bugs — they are documented and do not block shipping.

---

## Machine Guard

**Command:** `make operational-location-guard`  
**Integration:** Part of `make guardrails` and `make ci-local`

The guard checks:

1. No `GROUP BY` / `SELECT DISTINCT` on `locations.name` without `shed_id` or park co-grouping
2. All `operational_location_display` values match `DisplayName(shed, partition)` on known seeds
3. New tables with location-bearing concepts have a `partition_label` column

---

## Round 2 — 2026-08-07: the CONTRACT-vs-STRUCT class (found on a physical phone)

OL-1..OL-9 were all "the value is there but rendered/keyed wrong". This round is a
DIFFERENT class and the guard could not see any of it: **the value never left the
backend at all.** A partitioned shed rendered as a bare "Mandela 2" on the operator
drive list while the DB held `Part 3` correctly in two places.

### OL-10 — the OpenAPI schema declared fields the Go struct never carried
`contracts/openapi/app-api.yaml` -> `VaccinationExecutionRow` declared
`partition_label`, `source_shed_name` and `operational_location_display`. The Go
`ExecutionRow` carried none of them. Silent drift: the contract promised a location
the payload could not express, and every client faithfully rendered what it got.
**Rule:** a field in the schema is a claim about the payload. Adding it to the
contract without the struct is worse than omitting both -- it makes the gap invisible
to a reader of either file alone.

### OL-11 — the guard only inspected `*Request` schemas
`tools/agent-hooks/check-operational-location.mjs` gated its OpenAPI partition rule on
`/Request$/.test(header)`. Write paths were enforced; **every response schema was
unguarded**, which is where OL-10 shipped. An audit then found 9 more response schemas
carrying a shed with no partition.
**Rule:** a guard that only sees write paths does not enforce a display convention.

### OL-12 — presence-keyed rules cannot detect absence
Every other rule in that guard keys on partition being MENTIONED (a bad separator, a
'whole' leak, a name-keyed GROUP BY). A surface that forgot partitions entirely names
them nowhere, so no rule can fire. The guard's own header admitted this blind spot for
one rule; the structural check was never built.
**Rule:** for a MANDATORY field, the check must be "location-bearing => field present",
not "field present => field correct".

### OL-13 — three partition sources, and the screen read the stale one
`shed_partitions` (catalog of partitions that EXIST) and `goat_shed_partitions`
(per-goat placement) were both correct; the drive list read
`vaccination_drive_assignments.partition_label`, a SNAPSHOT that nothing re-derives
after a shed is partitioned. Weighing had already solved this
(`weighing/adapters/postgres/shed_partition_resolve.go` resolves from the catalog at
read time); vaccination trusted the stored column.
**Rule:** never trust a snapshot column for DISPLAY. Resolve from the catalog at read
time, or reconcile the snapshot when the catalog changes.

### OL-14 — no seed writes the `shed_partitions` catalog
Zero writers under `backend/cmd/`. The catalog is populated once by migration 000112
(backfilled FROM `goat_shed_partitions`) and never again, so any seeded partition is
invisible to every catalog-driven picker and empty partitions are unreachable as
shifting destinations. The seed's own invariant check asserted per-goat rows but never
a catalog row, so the gap was silent to its own validation.
**Rule:** a seed that creates a partitioned shed writes ALL THREE of catalog, per-goat
placement, and any assignment snapshot -- and asserts all three.

### OL-15 — name-keyed grouping that merges COUNTS, not just labels
`apps/admin-web/features/preventive-care-vaccination/command-board-view.tsx`
`buildShedGrid()` keyed by bare `shedName`, so two same-named sheds in different parks
AND two partitions of one shed collapsed into one row **with their animal counts summed
together**. OL-2 was the label version of this; this is the arithmetic version.
**Rule:** name-keying is a data-correctness bug, not a cosmetic one.

---

## Do-Not-Reopen Statement

This ledger documents defects that were surfaced but not all fixed in 2026-08-06. The decision to codify the convention (ADR) and add the guard is FINAL.

**Do NOT reopen this ledger to:**
- Relax the requirement for `partition_label` in new tables
- Allow name-keying (grouping by shed name instead of `shed_id` + park)
- Omit the partition from product display when one exists
- Skip the canonical display helper

**A reopening requires:**
- Proof that the worked examples (OL-2, OL-3) no longer apply
- Evidence that relaxing the rule does not reproduce those defects
- Maintainer approval of the new rule

Until then, the convention stands, the guard is mandatory, and the brief includes the rule.
