# Goat OS E2E Seed Validation Audit — 2026-07-19

**Date**: 2026-07-19  
**Repository Commit**: 83a2fdd4aa6284747de5d6a4164f0c1e094bada9  
**Database**: goatos-e2e-audit-pg (postgres://localhost:15544/goatos)  
**Tenant ID**: 00000000-0000-4000-8000-000000000001  

---

## Execution Summary

**Status**: BLOCKED at Stage 3 (Vaccination Source Seed)

| Stage | Command | Status | Exit Code | Duration | Finding |
|-------|---------|--------|-----------|----------|---------|
| 1 | Migrations (000001, 000002) | PASS | 0 | ~5s | Cleanstate baseline + vaccination capacity overflow policy |
| 2 | seed-dev-email-grants (founders) | PASS | 0 | ~2s | 5 founder/builder grants created |
| 3 | seed-roster-real (HRMS) | PASS | 0 | ~3s | 31 members, 33 positions, 6 leave windows |
| 4 | seed-vaccination-real (goats + history) | **FAIL** | 1 | — | **BLOCKER: Source data validation error** |
| 5+ | seed-shed-positions, seed-position-duties, seed-closeout | SKIPPED | — | — | Cannot proceed without Stage 4 completion |

---

## Stage 1: Migrations

**Command**: `go run ./cmd/migrate`  
**Environment**: GOATOS_ENV=local, DATABASE_URL=postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable  
**Status**: ✅ PASS

### Migrations Applied

| Migration | Filename | Result |
|-----------|----------|--------|
| 000001 | goatos_clean_slate_baseline.sql | Applied (~13,777 lines, includes all canonical schema) |
| 000002 | vaccination_capacity_overflow_policy.sql | Applied (vaccination.capacity overflow policy configuration) |

**Evidence**: Both migrations applied without error. Fresh Postgres 16 database now contains:
- Canonical tables (goats, locations, HRMS positions, protocols, obligations, completions, proofs, events)
- Baseline catalog entries (species, breeds, permission grants, protocol definitions)
- Baseline static configuration (vaccination matrix prototype, org structure, role catalog)

---

## Stage 2: Founder/Builder Grants (seed-dev-email-grants)

**Command**: `make seed-dev-email-grants`  
**Status**: ✅ PASS

**Output**:
```
pending email grant 32c6a5b6-70f7-4b96-949c-35312418eb08 active for abhishek@mesha.sg role ceo_internal tenant 00000000-0000-4000-8000-000000000001
pending email grant d333e07d-dad5-4407-83bd-94f35e6bf20d active for aryaman@mesha.sg role ceo_internal tenant 00000000-0000-4000-8000-000000000001
pending email grant 2a9800e2-8d96-445e-b440-97a50c61c3fb active for manju@mesha.sg role ceo_internal tenant 00000000-0000-4000-8000-000000000001
pending email grant e686172d-5295-4675-a2df-0cb731f883c8 active for manohark@mesha.sg role ceo_internal tenant 00000000-0000-4000-8000-000000000001
pending email grant 7bf830f6-adc3-4967-9f82-b8f9a88fd1bd active for ravi@mesha.sg role ceo_internal tenant 00000000-0000-4000-8000-000000000001
```

**Counts**:  
- Grants inserted: 5 (all five platform founders: ravi, manohark, manju, aryaman, abhishek)
- Role: ceo_internal
- Status: pending (awaiting email-based activation)

---

## Stage 3: HRMS/Roster Seed (seed-roster-real)

**Command**: `go run ./cmd/seed-roster-real -tenant-id {TENANT_ID} -source {SOURCE_DIR}`  
**Status**: ✅ PASS

**Output**:
```
normalized real roster:
  jun26_rows=42 mapping_rows=34 assignments_filled=33 unresolved=1
  members_with_grade=31 manual_seed_members=1 backup_groups_by_center=map[CBE:map[am1_backup:7 am2_backup:5 manager_backup:3] CPT:map[am1_backup:7 am2_backup:5 manager_backup:4]]
  attendance_rows=42 matched_members=30 unmatched_names=12 leave_windows=6 leave_days=35
WARNING: 1 unresolved assignment(s) -- slots seeded with position but null member/grade
seeded real roster:
  members_inserted=31 positions_inserted=33 leaves_inserted=6 department_matches=4
```

**Counts**:  
- Roster members seeded: 31 (Jun-26 reviewed roster)
- Positions inserted: 33 (staff member assignments)
- Leave windows: 6 (leave periods for Jun-26 attendance)
- Leave days: 35 total
- Attendance rows: 42 (matched to 30 members, 12 unmatched names — expected for partial mapping)
- Unresolved slots: 1 warning (position created with null member/grade — acceptable for dev seed)

**Note**: HRMS seeding is complete and ready for shed-ownership and position-duties wiring.

---

## Stage 4: Vaccination Source Seed (seed-vaccination-real) — **BLOCKER**

**Command**: `go run ./cmd/seed-vaccination-real -tenant-id {TENANT_ID} -source {SOURCE_DIR}`  
**Status**: ❌ **FAIL — CRITICAL BLOCKING FINDING**

### Error Output

```
seed: source animal "901007000504299" has DOB 2025-06-12 after entry_date 2025-05-24
exit status 1
```

### Root Cause

**Source Data Validation Error**: The goats.json source file contains **45 animals** with date-of-birth (DOB) **after** their herd entry date, which is logically impossible. The validation correctly rejects these rows because an animal cannot be born after it is purchased or enters the herd.

**Primary offender** (RFID 901007000504299):
- **DOB**: 2025-06-12 (June 12, 2025)
- **Purchase Date** (entry source): 2025-05-24 (May 24, 2025)
- **Delta**: +19 days (invalid)

**Scope of issue**:
- Total source animals: 1,311
- Animals with DOB > entry_date: **45 (3.4% of herd)**
- Pattern: Most (many of the first group) share identical DOB/entry date pair (2025-06-12 vs 2025-05-24), suggesting systematic data entry error or copy-paste issue

**Sample of invalid animals**:
| RFID | DOB | Entry Date | Entry Source | Days Invalid |
|------|-----|-----------|--------------|-------------|
| 901007000504299 | 2025-06-12 | 2025-05-24 | purchase | +19 |
| 901007000504250 | 2025-06-12 | 2025-05-24 | purchase | +19 |
| 901007000504295 | 2025-06-12 | 2025-05-24 | purchase | +19 |
| 901007000503955 | 2026-04-11 | 2025-07-15 | stage_entry | +271 |
| 901007000505164 | 2025-11-06 | 2025-11-03 | purchase | +3 |
| (... 40 more) | ... | ... | ... | ... |

### Contract Violation

Per `docs/runbooks/vaccination-seed-source-date-contract.md`, Section "Authoritative Per-Vaccine Anchor Order":

> Trusted DOB (date of birth) and herd-entry date are scheduling anchors. Missing scheduling-anchor checks are trigger-specific. A vaccine with no accepted administration history and the applicable path is procurement/adult primary requires entry date. An animal cannot have DOB after entry date — this is a fundamental identity constraint.

The seed-vaccination-real command validates this during source-data loading (before schema insertion) to ensure canonical goat identity rows respect biologically valid timelines.

### Attempted Remediation

**Flag Tested**: `-allow-partial-generation`  
**Result**: Does not bypass source validation. This flag allows the seed to continue when per-goat generation fails (e.g., missing DOB for a vaccine requiring age-based scheduling), but it does not bypass source-data identity validation (which is a prerequisite to schema insertion).

### Impact

**Severity**: 🔴 CRITICAL / BLOCKING  
**Scope**: Entire vaccination source seeding is blocked.  
**Downstream Blocks**:
- Stage 5: seed-shed-positions (cannot run without Stage 4 completing)
- Stage 6: seed-position-duties (blocked by Stage 5)
- Stage 7: seed-closeout and all proofs (blocked by Stages 5-6)

**Cannot proceed with**:
- Canonical goat seeding (blocked at source validation)
- Vaccination eligibility rollup recompute (no canonical goats)
- Obligation generation and sweeper closeout (no source obligations)
- Ownership completeness proofs (blocked by missing goats)
- Vaccination drive batching proofs (blocked by missing obligations)

### Recommendation

**Fix Required in Source Data**:  
1. Audit `/Users/ravi/mesha/source-material/vgoats-seed/goats.json` for all animals with DOB > entry_date
2. Correct the dates (or remove the offending records if they are test/placeholder rows)
3. Re-validate that all animals have entry_date ≤ DOB (or DOB is NULL for unknown-age animals)
4. Re-run the seed validation with corrected data

**Source File**: `/Users/ravi/mesha/source-material/vgoats-seed/goats.json`  
**Offending RFID**: 901007000504299

---

## Stages Not Executed

### Stage 5: Shed Position Ownership (seed-shed-positions)
**Status**: ⏭ SKIPPED (blocked by Stage 4)  
**Command** (would be): `go run ./cmd/seed-shed-positions -tenant-id {TENANT_ID} -mapping {MANAGER_MAPPING} -strict`

### Stage 6: Position Duties (seed-position-duties)
**Status**: ⏭ SKIPPED (blocked by Stage 5)  
**Command** (would be): `go run ./cmd/seed-position-duties -tenant-id {TENANT_ID}`

### Stage 7: Seed Closeout & Proofs (seed-closeout)
**Status**: ⏭ SKIPPED (blocked by Stage 6)  
**Command** (would be): `bash tools/dev/seed-closeout.sh`  
**Would Include**:
- Vaccination eligibility rollup recompute
- Goat-shed integrity proof
- Vaccination obligation generation
- Obligation sweeper (batching + drive clubbing)
- Vaccination drive clubbing capacity proof

---

## Mandatory Acceptance Gates (UNEXECUTED)

The following gates could not be executed due to Stage 4 blocking. They are documented here for reference:

### Gate 1: Owner Completeness
**Query** (not executed):
```sql
-- Active sheds without manager position
SELECT COUNT(*) FROM sheds s
WHERE s.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND s.status = 'active'
  AND NOT EXISTS (
    SELECT 1 FROM positions p
    WHERE p.tenant_id = s.tenant_id
      AND p.shed_id = s.shed_id
      AND p.position_type = 'manager'
      AND p.status = 'active'
  );
```
**Expected**: 0  
**Status**: ⏭ UNEXECUTED (no goats seeded)

### Gate 2: Micro-Drive Evidence
**Report** (not executed):  
`make vaccination-drive-clubbing-db-proof`  
**Expected**: Each micro-drive (1-2 animals) documents why no larger group could be formed within safe windows.  
**Status**: ⏭ UNEXECUTED (no obligations seeded)

---

## Database State

**Throwaway Container**: `goatos-e2e-audit-pg`  
**Port**: 15544  
**DB Name**: goatos  
**User/Pass**: postgres:goatos

### Schema Present (Confirmed)
- Canonical tables (goats, locations, HRMS, obligations, completions, proofs, etc.)
- Baseline catalog (species, breeds, permissions, protocol definitions)
- Two migrations applied (baseline + capacity overflow)

### Data Seeded (Partial)
- ✅ Founder email grants: 5 rows
- ✅ HRMS roster members: 31 rows
- ✅ HRMS positions: 33 rows
- ✅ HRMS leave windows: 6 rows
- ❌ Goats: 0 rows (validation blocked)
- ❌ Vaccination source history: 0 rows
- ❌ Shed manager positions: 0 rows
- ❌ Obligations: 0 rows
- ❌ Vaccination batches: 0 rows

---

## Findings Summary

### Finding 1: Source Data Identity Validation Error (CRITICAL/BLOCKING)

**ID**: F001  
**Severity**: 🔴 CRITICAL  
**Category**: Source Data Bug / Data Validation  
**Status**: UNRESOLVED  

**Description**: Goats source file contains animal RFID 901007000504299 with DOB (2025-06-12) after entry_date (2025-05-24), violating identity constraints.

**Impact**: 
- Blocks all vaccination source seeding
- Blocks all downstream seed stages (positions, duties, closeout, proofs)
- E2E audit cannot complete

**Resolution Needed**: 
1. Audit source data for all DOB > entry_date violations
2. Correct or remove offending records
3. Re-validate source before re-running seed

**Evidence Location**: Source file `/Users/ravi/mesha/source-material/vgoats-seed/goats.json`

---

## Conclusion

The E2E seed validation audit **cannot proceed past Stage 3 (HRMS)** due to a source data validation error in Stage 4 (Vaccination Source Seeding). The error is a logical date inconsistency (animal DOB after entry date) that violates the vaccination-seed contract's identity anchor requirements.

**Migrations and foundational seeding (grants, HRMS) completed successfully**, proving that:
- Schema migrations apply cleanly (000001, 000002)
- Founder grant seeding works
- HRMS roster import and position seeding works

**No further validation is possible without resolving the source data bug.**

---

**Evidence File Generated**: /Users/ravi/mesha/goatos/context/execution/e2e-audit-2026-07-19-seed-evidence.md  
**Database**: Still running on port 15544 for manual inspection (cleanup: `docker rm -f goatos-e2e-audit-pg`)

---

# RERUN — 2026-07-19 (main = e451ad93, blocker fix landed)

**Commit**: e451ad93 "fix: per-origin seed entry semantics + sanctioned false-DOB disposition"
**DB**: fresh goatos-e2e-audit-pg (postgres:16-alpine, port 15544; previous container removed)
**Tenant**: 00000000-0000-4000-8000-000000000001

## Stage Table (rerun)

| # | Stage | Command | Exit | Key counts |
|---|-------|---------|------|-----------|
| 1 | Migrations | `go run ./cmd/migrate` (GOATOS_ENV=local) | 0 | 000001 baseline + 000002 vaccination_capacity_overflow_policy both applied cleanly on fresh DB |
| 2 | Founder grants | `make seed-dev-email-grants` | 0 | 5 ceo_internal grants |
| 3 | HRMS roster | `seed-roster-real` | 0 | members=31, positions=33, leaves=6, department_matches=4, 1 unresolved slot (known) |
| 4 | Vaccination source seed | `seed-vaccination-real -null-false-dob -expect-null-false-dob=42` | 0 | animals=1311, parks=2, sheds_resolved=100(+16 created), completions_history=3836, reconciled=3836 (later_administrations=447, unresolved=0), kernel generated=4078 deferred=723 failed_goats=0 suppressed_trusted=3009, **dob_nulled=42**, reconciliation all-zero defects, post_seed_analyze completed |
| 4a | DOB disposition sidecar | — | — | `/Users/ravi/mesha/source-material/vgoats-seed/seed-dob-disposition-2026-07-19.json`: **42 entries**, disposition=dob_nulled_false, includes original_dob/entry_date/entry_source/delta_days |
| 5 | Shed positions | `seed-shed-positions -strict` | 0 | active_sheds=170, reviewed_seats=116, park_fallback=54, manager gaps=0, backup gaps=0, seats_inserted=170 |
| 6 | Position duties | `seed-position-duties` | 0 | duties_inserted=16 (shed_manager unmapped skipped — known warning) |
| 7 | Seed closeout | `tools/dev/seed-closeout.sh` | **1** | seed-state-check OK (latest_state=verified); goat-shed integrity **passed (1192\|1192)** twice; eligibility rollup grains=402 eligible=963; generation idempotent re-run (generated=0, suppressed_trusted=3009); sweeper batches=47 obligations=2329 (park_batches=45/2327); in-window unbatched leftover=0; **clubbing proof FAILED (Finding F002)** |

Note: first closeout attempt aborted with `psql: command not found` (host PATH); environmental fix = `PATH=/opt/homebrew/opt/libpq/bin:$PATH`, rerun succeeded to the proof stage.

## Proofs and gates

| Gate | Result | Evidence |
|------|--------|----------|
| Migration 000002 fresh-baseline apply | PASS | `migration_applying ... 000002_vaccination_capacity_overflow_policy` no error |
| Goat-shed integrity | PASS | `goat-shed-integrity proof: passed (1192|1192)` (after seed and after generation) |
| Unbatched-due in window | PASS | 0 vaccination scheduled/due obligations unbatched through 2026-12-31 |
| Owner completeness | PASS | 116 sheds with goats, 0 without an active held position seat; strict preflight: 0 manager gaps, 0 backup gaps; 6 active backup slots |
| Judge 42-animal acceptance | PASS | 42/42 nulled goats resolved; herd=1311; obligations by trigger: post_arrival (177 completed/14 deferred/30 scheduled), after_previous_completion (41 deferred/48 scheduled), birth_age ACTIVE=**0**. Observation: 4 birth_age COMPLETED rows exist among the 42 — imported sheet history reconciled under kid-course rules despite nulled DOB; these are accepted history, not generated open work, but worth review. |
| Capacity-aware clubbing proof (first live run) | **FAIL — Finding F002** | Script SQL executed without error (no SQL bug). Proof asserts: small drive 2026-12-21 Coimbatore kid_mixed animals=2 (Godel 2, Yashoda 6; due_from=2026-12-19, safe_until=2027-01-18) has a compatible same-park FMD drive 2026-12-31 (animals=100, 17 sheds) inside its safe window — i.e. the sweeper left an avoidable micro-drive unclubbed. |
| Micro-drive evidence | **FAIL** (by F002) | 5 batches with <=2 animals: 2026-08-02(2), 2026-08-29(1), 2026-09-20(2), 2026-11-03(1), 2026-12-21(2). At least the 2026-12-21 one is proven avoidable by the proof script. |

## Capacity observations (config: max_per_day=100, scope=tenant, buffer=7d, policy=split_within_safe_window_last_safe_may_exceed_cap)

- Per-park per-day batches are ≤100 except **2026-08-12 Channapatna = 166 in a single batch** (may be sanctioned by last-safe-may-exceed policy; flag for review).
- Tenant-wide days exceeding 100: 2026-07-22 (200), 2026-07-23 (200), 2026-07-24 (167), 2026-07-25 (200), 2026-08-12 (230), 2026-12-19 (196). Cap scope is `tenant`, so if the cap is tenant-wide these six days exceed it; if operationally per-park, only 2026-08-12 does. Needs a product ruling.

## Findings (rerun)

- **F002 (REAL, from first live run of rewritten capacity-aware clubbing proof)**: `tools/dev/check-vaccination-drive-clubbing-proof.sh` fails — sweeper produced a 2-animal kid_mixed drive on 2026-12-21 (Coimbatore) although a compatible 100-animal FMD drive on 2026-12-31 sits inside the safe window (due_from 2026-12-19 → safe_until 2027-01-18). Either the sweeper's clubbing missed a legal merge, or the proof's compatibility test is broader than the sweeper's (kid_mixed vs FMD protocol-lineage compatibility). Not hacked around; closeout intentionally left failed.
- **F003 (observation)**: 4 completed birth_age history rows reconciled for goats whose false DOB was nulled — kid-course rule mapping used the pre-null DOB path during history reconciliation. Accepted history only; zero active birth_age work.
- **F004 (observation)**: capacity cap scope=tenant vs per-park batching ambiguity; 6 tenant-days >100 animals, one single-park batch of 166.

**Overall**: seed chain end-to-end GREEN through generation/sweeper with all hard seed gates passing; the run is amber solely on the clubbing-proof failure (F002), which is exactly the class of defect this proof was rewritten to catch.
