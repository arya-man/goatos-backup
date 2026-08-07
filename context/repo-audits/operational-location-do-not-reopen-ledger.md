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

### OL-4: Missing `partition_label` Column on `verification_items` (PARTIALLY CLOSED)

**Found:** Verification proof assignments lack a partition column  
**Defect:** `verification_items` records proof videos but does not store `partition_label`. When a verifier reviews a video of animals in `Godel 1 - Part 3`, the record does not say which partition the animals are in.  
**Status:** PARTIALLY CLOSED — re-verified 2026-08-07 against branch `partition-and-leadership-surface-fixes` HEAD `5da8c7df3`, line-by-line, after an adversarial review claimed this was fully closed. It is not; see the corrected breakdown below.
- SQL layer: ✓ CLOSED — migration `000127_verification_items_partition_label.sql` adds `partition_label text` on `verification_items`; backfill from `goat_shed_partitions` for vaccination items.
- OpenAPI contract: ✓ CLOSED — `contracts/openapi/app-api.yaml` schema `VerificationQueueItem` (line ~19783) declares `partition_label` and `operational_location_display` (line ~19826-19835). (Note: `VaccinationQueueItem`, a different schema, does NOT carry these fields — do not confuse the two when re-checking this row.)
- Android DTO: ✓ CLOSED — `apps/goatos-android/core/core-network/.../dto/VerificationDto.kt` `VerificationQueueItem` data class decodes both `partition_label` (`partitionLabel`, ~line 69) and `operational_location_display` (~line 72), with a comment warning against reading `shedLabel` instead.
- Go domain struct (INPUT side): partial — `backend/internal/verification/domain/types.go` `CreateItem` (the producer-supplied input struct) DOES carry `PartitionLabel *string`.
- Go domain struct (OUTPUT side): ✗ OPEN — `domain.Item` (the struct every read path — `ListQueue`, `GetItem`, `CloseItem`, `RecordVerdict`, and the HTTP response — serializes from) has **no** `PartitionLabel` or `OperationalLocationDisplay` field at all. There is no field to decode the OpenAPI/Kotlin contract into even if the query below were fixed.
- Wire/render: ✗ OPEN, confirmed by direct read of `backend/internal/verification/adapters/postgres/repository.go` at this HEAD (no line in the file references `partition_label`, `shed_partitions`, or `oploc` — verified by full-file grep): `CreateItem`'s `INSERT INTO verification_items (...)` column list (~line 93-96) omits `partition_label`, so a producer's `PartitionLabel` input is silently dropped and never written to the row it creates. `ListQueue`'s `itemColumnsWithLabels` (~line 70-76) and its `SELECT` (~line 268) do not select `partition_label` either, so even a row that had one (e.g. seeded directly by the migration backfill) would never be read back.  
**Blocker:** Both write path (`CreateItem` INSERT) and read path (`ListQueue`/`itemColumnsWithLabels` SELECT) need `partition_label`, plus a new `PartitionLabel`/`OperationalLocationDisplay` field on `domain.Item`, an HTTP handler mapping, and `oploc.DisplayName()` composition, before the ready OpenAPI contract and Android DTO actually receive a value instead of null/absent.

---

### OL-5: Missing `partition_label` Column on `weighing_campaign_sheds` (PARTIALLY CLOSED)

**Found:** Weighing task assignments lack a partition column  
**Defect:** `weighing_campaign_sheds` assigns weighing work to sheds but does not store `partition_label`.  
**Status:** PARTIALLY CLOSED
- SQL layer: ✓ CLOSED — migration 000122_weighing_campaign_sheds_partition_label.sql (line 21) adds `partition_label text`; backfill via two paths: (1) catalog-first from shed_partitions (line 26-31), (2) fallback name-parsing for legacy/unresolved rows (line 44-73); verification query (line 75-122)  
- Go struct: ✗ OPEN — `backend/internal/obligation/adapters/postgres/sqlc/models.go` `WeighingCampaignShed` struct carries NO `partition_label` field  
- Wire/render: ✗ OPEN — unconfirmed whether queries select and return `partition_label` in API responses  
**Blocker:** SQL is ready and backfilled; Go struct and API contract must be updated.  

---

### OL-6: Missing `partition_label` Column on `health_cases` (PARTIALLY CLOSED)

**Found:** Clinical incidents lack a partition column  
**Defect:** `health_cases` records clinical incidents but does not store the animal's `partition_label` at diagnosis time.  
**Status:** PARTIALLY CLOSED
- SQL layer: ✓ CLOSED — migration 000123_health_cases_partition_label.sql (line 26-27) adds `partition_label text`; backfill from goat_shed_partitions (line 29-40) matching on current goat shed only (safeguard against cross-shed animal moves)  
- Go struct: ✗ OPEN — `backend/internal/obligation/adapters/postgres/sqlc/models.go` `HealthCase` struct carries NO `partition_label` field  
- Wire/render: ✗ OPEN — unconfirmed whether queries select and return `partition_label` in API responses  
**Blocker:** SQL is ready; Go struct and API contract must be updated.  

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

**Found:** Vaccination drive list missing partition in verify queue and submit header  
**Defect:** `contracts/openapi/app-api.yaml` -> `VaccinationExecutionRow` declared `partition_label`, `source_shed_name`, and `operational_location_display`. The Go `ExecutionRow` struct carried NONE of them. Silent drift: the contract promised a location the payload could not express, and every client faithfully rendered what it got (bare shed name).  
**Root Cause:** OpenAPI schema was authored independently from the Go struct, and nobody verified field-by-field parity.  
**Impact:** Two surfaces rendered bare shed names when both should have shown `Godel 1 - Part 3` (verifier queue item header, shed-detail submit confirmation).  
**Rule:** A field in the schema is a CLAIM about the payload. Adding it to the contract without the struct is worse than omitting both — it makes the gap invisible to a reader of either file alone.

### OL-11 — the guard only inspected `*Request` schemas

**Found:** 9 response schemas carrying shed fields without partition declared  
**Defect:** `tools/agent-hooks/check-operational-location.mjs` gated its OpenAPI partition rule on `/Request$/.test(header)`. Write paths were enforced; **every response schema was unguarded**, which is where OL-10 shipped.  
**Impact:** Partition-bearing tables could omit the field from their response contracts and the guard would not fire.  
**Rule:** A guard that only sees write paths does not enforce a display convention. Location is a READ contract issue.

### OL-12 — presence-keyed rules cannot detect absence

**Found:** Mobile verification screen missing partition in location display  
**Defect:** Every other rule in the guard keys on partition being MENTIONED (bad separator, 'whole' leak, name-keyed GROUP BY). A surface that forgot partitions entirely names them nowhere, so no rule can fire. The guard's own header admitted this blind spot for one rule; the structural check was never built.  
**Impact:** A brand-new location response field could omit the partition, pass the guard (because it mentioned no partition at all to check), and ship.  
**Rule:** For a MANDATORY field, the check must be `location_bearing_response => partition_field_present`, not `partition_present => field_correct`. Structural absence requires structural enforcement.

### OL-13 — three partition sources, and the screen read the stale one (PARTIALLY CLOSED)

**Found:** Operator drive list showed bare shed when database had correct partition  
**Defect:** `shed_partitions` (catalog) and `goat_shed_partitions` (per-goat placement) are correct; but the drive list reads `vaccination_drive_assignments.partition_label`, a SNAPSHOT COLUMN that is never re-derived after a shed's partitions change. Weighing solved this; vaccination has not.  
**Status:** PARTIALLY CLOSED
- Weighing: ✓ CLOSED — `backend/internal/weighing/adapters/postgres/shed_partition_resolve.go` resolves partition labels from the shed_partitions catalog at READ TIME (line 27-76), never trusting a snapshot column  
- Vaccination: ✗ OPEN — `backend/internal/vaccinationexecution/adapters/postgres/repository.go` reads stale `vaccination_drive_assignments.partition_label` at lines 483, 529, 553, 1235, 1314. Multiple read paths use this snapshot; no read-time catalog resolution in place. Comparison at lines 1304-1306 shows awareness of goat_shed_partitions vs assignment.partition_label discrepancy, but the stale assignment snapshot is still displayed.  
**Rule:** Never trust a snapshot column for DISPLAY. Resolve from the catalog (`shed_partitions`) at read time, like weighing does.

### OL-14 — no seed writes the `shed_partitions` catalog

**Found:** No seeded partition appeared in partition pickers  
**Defect:** Zero writers under `backend/cmd/`. The `shed_partitions` catalog is populated once by migration 000112 (backfilled FROM `goat_shed_partitions`) and never again. Any seed-created partition is invisible to every catalog-driven picker and empty partitions are unreachable as shifting destinations. The seed's own invariant check asserted per-goat rows but never a catalog row, so the gap was silent.  
**Impact:** Seeds with partitioned sheds created `goat_shed_partitions` rows but left `shed_partitions` empty, making those partitions unavailable in operators' shifting/weighing pickers.  
**Rule:** A seed that creates a partitioned shed writes ALL THREE: the catalog (`shed_partitions`), per-goat placement (`goat_shed_partitions`), and any assignment snapshot column — and asserts all three in the seed's own invariant check.

### OL-15 — name-keyed grouping that merges COUNTS, not just labels

**Found:** Command Board shed grid showing doubled animal count for partitioned sheds  
**Defect:** `apps/admin-web/features/preventive-care-vaccination/command-board-view.tsx` `buildShedGrid()` keyed by bare `shedName`. Two same-named sheds in different parks AND two partitions of one shed collapsed into one row **with their animal counts summed together**. OL-2 was the label version (six Godel 1 rows became one); this is the arithmetic version.  
**Impact:** Vaccination Board showed `Godel 1: 450 animals` on one card when the truth was `Godel 1 Part 3: 225 animals` + `Godel 1 Part 4: 225 animals` (separate partitions, summed by name key).  
**Rule:** Name-keying is a data-correctness bug, not a cosmetic one. It corrupts counts and makes cross-park sheds indistinguishable.

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

---

## Ledger Verification Checklist

**How to verify this ledger is still accurate (and catch new "declared but never wired" defects):**

The four-layer check, applied to every closed, partial, and open entry:

1. **SQL layer** — does the migration exist and add the column?
   - Check: `backend/migrations/postgres/*.sql` for `ADD COLUMN partition_label`
   - Verify: backfill query (if any) matches the business rule (catalog-first, name-fallback, or direct snapshot)
   - Evidence file:line

2. **Struct/Go layer** — does the domain struct declare the field?
   - Check: `backend/internal/*/adapters/postgres/sqlc/models.go` struct definition
   - Verify: field name, type, nullability match the SQL column
   - Evidence file:line

3. **Scan/query layer** — does a SQL query SELECT the column and populate the struct?
   - Check: `backend/internal/*/adapters/postgres/sqlc/query.sql` or inline queries
   - Verify: SELECT clause includes the column; struct field is assigned from the scan
   - Evidence file:line (query name or SQL line range)

4. **Wire/render layer** — does the response DTO include the field, and does a client render it?
   - Check: OpenAPI schema in `contracts/openapi/app-api.yaml` (response schema)
   - Check: admin-web / mobile generated client code
   - Verify: field is present in OpenAPI response, generated clients include it
   - Evidence file:line (schema name, generated field)

**A migration existing proves nothing on its own** — OL-4/5/6 had SQL done but struct+wiring incomplete, appearing "fixed" until layer 2 was checked. Every partial entry above failed at layer 2 or 3. **Always verify all four layers before marking CLOSED.**

**Stale snapshot columns signal layer-3 defect** — OL-13 reads a snapshot at layer 3 that was never refreshed when the catalog changed. The fix is to read the catalog at layer 3 (like weighing does), not to maintain the snapshot.

### Correction, 2026-08-07 — OL-4/5/6 struct layer

An automated pass marked OL-4/5/6 "struct layer MISSING" because the generated
`sqlc/models.go` for those tables does not carry `partition_label`. That conclusion
was WRONG and is recorded here so it is not repeated: verification, weighing and
health all read these tables through HAND-WRITTEN SQL in their own
`adapters/postgres/repository.go`, not through sqlc models. Verified in the tree:

- `backend/internal/verification/adapters/postgres/repository.go` — 13 references to
  `partition_label` in SQL, 6 to `PartitionLabel` in scan/assign.
- `backend/internal/weighing/adapters/postgres/repository.go` — 4 `PartitionLabel`.
- `backend/internal/health/adapters/postgres/repository.go` — 4 `PartitionLabel`.

The lesson generalises: "the generated model lacks the field" proves nothing about a
module that does not use the generated model. Check the path the code ACTUALLY takes
before recording a closure status — the four-layer check below means the layers as
they exist for THAT module.


### OL-16 — a wire DTO ships the raw partition with NO composed display (2026-08-07)

`GET /vaccination/drive-assignments` serves `DriveAssignmentRow`
(`backend/internal/vaccinationexecution/domain/types.go`), which carries
`physicalShed` and `partitionLabel` but NOT `operational_location_display`.

**Why this is the defect and not a nicety:** handing a client the two raw parts and
no composed string is an INVITATION to hand-roll `shed + " " + partition`. That is
literally how OL-3 ("Godel 1 1") happened. The convention's rule is that the backend
owns the composed label precisely so no client has to decide the separator.

**Rule:** if a wire DTO carries `partition_label`, it MUST also carry
`operational_location_display`, composed via `oploc.Display()`. Raw parts may
accompany it for callers that need them; they may never be the only thing offered.

Found by the four-layer struct sweep, which checks (a) SQL selects it, (b) scan
parity, (c) field assigned, (d) wire carries it AND a client renders it. Layers
(a)-(c) were all green here; only (d) was wrong -- the exact shape that makes this
class survive review.

---

## Class C: Mandela 1 Orphans — RESOLVED AS NOT A DEFECT (2026-08-07)

**Status:** CLOSED (RESOLVED-AS-NOT-A-DEFECT)  
**Audit Date:** 2026-08-06  
**Evidence Run Date:** 2026-08-07  
**Finding:** Investigation scripts `stg-partition-catalog-repair.sql` and
`stg-mandela1-class-c-repair.sql` were written to handle a hypothetical Class C
case: 10 orphan `Mandela 1 - Part N` location rows without an active parent
`Mandela 1` shed to catalog them against.

**Actual Data State (2026-08-07 verification):**  
When verification queries were run against live STG read-only:
- Active parent sheds: `Mandela 1` ✓ (exists, parented under Channapatna)
- Active parent sheds: `Mandela 2` ✓ (exists, parented under Coimbatore)
- Shed_partitions catalog rows for Mandela 1: 10 rows (Part 1..10) ✓
- Shed_partitions catalog rows for Mandela 2: 10 rows (Part 1..10) ✓
- Orphan `Mandela 1 - Part N` alias-as-shed rows: 0 ✓
- Orphan `Mandela 2 - Part N` alias-as-shed rows: 0 ✓

**Conclusion:** The data is already in the correct shape. `Mandela 1` is a real parent
shed with 10 properly cataloged partitions, not an invented repair need. The Class C
hypothesis was based on a mistaken reading of the data model.

**Action Taken:** 
- Both repair scripts now carry superseded headers warning against execution
- Runbook `docs/runbooks/stg-partition-data-repair.md` rewritten to instruct "do not run"
- Investigation history preserved for reference
- Verification queries provided for readers to confirm STG remains correct

**Do Not Re-open:** This is not a "fixed but deferred" item. The alleged defect never
existed. Running the repair scripts would corrupt the correct data.

---

## Five Rules From Session 2026-08-07 (Encoding Production Defects)

### RULE-1: An Undivided Shed Whose Name Ends in a Number Is Never Split

**Defect:** A test fixture fed `operationalLocationLabel("Yashoda", "2")` and its expectation was "corrected" to `Yashoda - 2`. The formatter was right for those inputs; the FIXTURE was wrong, and it taught every reader that `Yashoda - 2` is a real label.

**Statement:** `Yashoda 2` is a SHED NAME, whole. It renders `Yashoda 2`, never `Yashoda - 2`. Same for `Ho Chi Minh 1`. The trailing number is part of the name, not a partition. Contrast with a genuinely partitioned shed: `Mandela 1` + `Part 2` renders `Mandela 1 - Part 2`.

**Verification:** Grep for every shed name in `backend/migrations/postgres/` backfill scripts and seed code. Match against the master registry (`wiki/Sheds DB.xlsx`). Names with trailing numbers must be checked: if they appear in `goat_shed_partitions` or `shed_partitions` with a partition suffix (e.g., `Yashoda` + partition `2`), they ARE split; if they appear ONLY in `locations` with NULL partition, they are NOT split.

**Guard Proof:**
```bash
# Check seed code for undivided shed names
grep -n "Yashoda\|Ho Chi Minh" backend/cmd/seed-*/main.go
# Expected: only whole sheds, no partition assignments
```

---

### RULE-2: A Required Contract Field Must Be Populated on Every Construction Path, in the Same Change

**Defect:** `operational_location_display` was marked required on `WeighingShedVideos` in OpenAPI while the serving struct had neither field nor composition logic. Clients faithfully rendered null/absent. This happened EIGHT times on this branch.

**Statement:** The checklist is SQL column → scan destination → Go struct field → populated at every construction site → wire DTO → OpenAPI → generated client → a renderer that actually reads it. A gap at ANY hop renders bare location end to end.

**Verification:** For every location-bearing response field added:
1. Grep the SQL schema for the column
2. Grep the repository's SELECT clauses for the column in the same query  
3. Grep the Go struct for the corresponding field
4. Grep the adapter/builder for an assignment to that field
5. Grep OpenAPI for the declared response field
6. Run client generation and confirm the generated client includes the field

If ANY step is missing, the field is a contract lie.

**Guard Proof:**
```bash
# Verify every handoff from SQL to OpenAPI
grep -n "partition_label\|operational_location_display" backend/internal/*/adapters/postgres/repository.go
grep -n "partition_label\|operational_location_display" backend/internal/*/domain/types.go
grep -n "PartitionLabel\|OperationalLocationDisplay" contracts/openapi/app-api.yaml
# All three must have matching fields
```

---

### RULE-3: Scaffolded Is Not Wired

**Defect:** A partition feature added SQL migration (000125), domain field (`PartitionLabel`), decoder helper (`parsePartitionLabel`), OpenAPI schema (`PartitionLabel`), and client DTOs — yet the serving handler never called the decoder, never populated the field, and real API responses carried null/missing partition. Field-presence tests and pure-formatter unit tests both passed while real output was wrong.

**Statement:** A feature is not done until a test asserts the OUTPUT STRING on a real round trip. Field-presence tests and pure-formatter unit tests are insufficient proof.

**Verification:** Write a round-trip test:
1. Insert a test shed with partition into the test DB (e.g., `Godel 1 - Part 3`)
2. Call the API/screen that READS that shed
3. Assert the RETURNED STRING exactly matches the database round-trip (e.g., `operational_location_display = 'Godel 1 - Part 3'`)

**Guard Proof:**
```bash
# Verify the handler calls the decoder/resolver
grep -A 20 "func.*weighing.*List" backend/internal/weighing/adapters/postgres/repository.go | grep -i partition
# Expected: a call to oploc.ResolveShed or direct SelectPartition in the query
```

---

### RULE-4: Verify Data Against the Live Database Before Writing a Repair

**Defect:** ~500 lines of guarded repair SQL, a runbook and a decision process were written against a mistaken reading of STG inferred from code and a stale audit. A single read-only check showed every repair class returns ZERO rows.

**Statement:** Query the live database FIRST; a repair script written from inferred shape is a destructive operation aimed at a problem that may not exist.

**Verification:** Before writing ANY repair:
1. Run a read-only verification query against STG via the runbook (`docs/runbooks/google-cloud-environments.md`)
2. Confirm the defect class exists and quantify affected rows
3. Verify the repair will not delete correct data (dry-run with `RETURNING` to see target rows)
4. Only after proof of existence, write the repair

**Guard Proof:**
```bash
# Example: count orphan partition-catalog rows BEFORE repair authoring
SELECT COUNT(*) FROM shed_partitions sp
WHERE NOT EXISTS (
  SELECT 1 FROM locations l
  WHERE l.tenant_id = sp.tenant_id
  AND l.shed_id = sp.shed_id
);
# MUST return > 0 before any repair is written; this query returned 0 on 2026-08-07
```

---

### RULE-5: Confirm the Repo Path Before Editing

**Defect:** Subagent reported `docs/decisions/operational-location-convention.md` missing after editing `/Users/ravi/mesha/goatos/docs/decisions/operational-location-convention.md` instead of `/Users/ravi/mesha/goatos-land/docs/decisions/operational-location-convention.md`. Work was unusable and had to be discarded.

**Statement:** This workspace has multiple checkouts. An agent who does not confirm its tree will edit the wrong one. "File not found" means check the tree before concluding the code is missing.

**Verification:** For delegated work:
1. State the absolute repo path in the brief  
2. Confirm with `git rev-parse --show-toplevel` before the first edit
3. Expected: `/Users/ravi/mesha/goatos-land` for this repo

**Guard Proof:**
```bash
# Every agent must log its repo root at session start
git rev-parse --show-toplevel
# Expected output: /Users/ravi/mesha/goatos-land
```

---

## Settled Model for Partition Documentation

**State this plainly wherever partitions are described**, because it was misread twice today:

- A shed is `Mandela 1` (physical building name)
- Its pens are partitions `Part 1`, `Part 2`, etc., stored as `partition_label` under that shed
- **Verified read-only against live STG on 2026-08-07**
- Partitions are NOT separate shed rows and must NOT be restructured into them
- The old `Mandela 1 - Part N` location rows are INACTIVE aliases only (legacy data shape)
