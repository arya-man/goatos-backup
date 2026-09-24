# Vaccination age-floor invariant - 2026-09-24

## Close-out evidence (2026-09-24, rebased on origin/main)

- Triage: the full DB packages (obligation, vaccination, vaccinationexecution, migrations) were run on
  the branch and on origin/main against the disposable OCI container. origin/main alone fails 159
  tests; only 11 failed on the branch and not on main. Fixed: repeat-cycle fixtures (seed an
  accepted previous dose), the guard-agreement assertion (post_arrival catch-up without an arrival
  anchor is approved), and a command-board plan guard (drawer pen/park names are now PK lookups).
  The P2-18 test and migration 000401 were removed: 000401 caused a verification lock-order
  deadlock, and both are follow-up scope.
- Final: `go test ./internal/obligation/adapters/postgres ./internal/vaccination/adapters/postgres
  -count=1 -json` at 1800cd0bf: 116 failures, all present on origin/main; the 2 extra were pgtest
  template-lock timeouts under parallel load and pass alone (129.5s).
  `./internal/vaccinationexecution/adapters/postgres`: no failures beyond main; the plan guard passes.
- sqlc: `sqlc vet` + `sqlc diff` (v1.29.0) clean.
- Judges: CLINICAL SIGN-OFF at 0ef5cfed7; PERSISTENCE SIGN-OFF at 1800cd0bf (the only delta is the
  000401 removal plus the drawer SQL rewrite).
- Follow-up adds: order completion acceptance against guarded writes (lock order); remove the
  unreachable `completion` race branch.

## READ FIRST - next session: close this fast, do not go in circles

**The bug:** generation moved one 8-day-old animal's ET+TT dose (RFID `901007000506144`, DOB
2026-09-16) to 2026-09-24, before its DOB + 4-week floor of 2026-10-14. The fix is: no vaccination
row may ever be saved before its clinical floor (DOB/arrival + offset, previous dose + gap), on any
write path. That core guard is DONE on this branch.

**What went wrong in the last session (don't repeat it):**
- Scope creep. Every judge round found another edge case (lock order, carry-over speed, extra
  race tests) and each was sent straight to a builder instead of asking whether it belonged in
  this PR. Most of them never make a vaccination date wrong.
- Builders and judges chased each other. Judges reviewed half-finished working trees, found issues
  in them, and started another round.
- ~130 existing obligation integration tests were already failing at session start (fixtures
  without DOB, now rejected by this PR's fail-closed anchor rule). This was found late instead of
  being triaged first.
- Too many parallel builders in one package made integration slow.

**Rules for the next session:**
1. Scope is frozen: the original age-floor bug plus the 5 P1 blockers below (all implemented).
   Only a P0/P1 that makes a vaccination date or vaccine choice WRONG blocks this PR. Everything
   else (performance, lock order, extra races) goes into the follow-up list at the bottom. Don't
   implement it here.
2. First run the full OCI suite on the pushed head, and triage failures against origin/main
   before writing any product code.
3. Fix test fixtures to be clinically valid; never weaken the guard.
4. Judge only the exact pushed SHA, once clinical and once persistence, after tests are green.
   No continuous judging of a moving working tree.
5. At most 2 builders, on disjoint files.
6. Then rebase on origin/main (main has 000399; regenerate sqlc schema.sql copies), `make land-main`,
   deploy, repair the STG row, and read it back.

**Decision for the next session:** the last commit (38281d60a, WIP) contains unverified hardening:
the shared anchor package, the start of set-based carry-over, migration 000401 (completion locks
the goat), race tests and fixture edits. Either verify it quickly on OCI or revert the parts that
are follow-up scope. Don't let it grow.

**Follow-up PR (NOT this PR):** set-based carry-over + goat chunking (P1-13/P2-17), lock ordering
(P1-16, P2-20, P2-22, P2-23), in_progress carry-over candidates (P2-21), GuardRejected persisted on
vaccination_generation_runs, GuardRejected alerting.

## Scope

- Prevent vaccination generation/reconciliation/reschedule from moving a birth-age dose before DOB + rule offset.
- Repair the real STG ET+TT obligation for RFID `901007000506144` after the code guard is ready.
- Prove repeated sweeper reconciliation cannot recreate the early row.
- Make the authored `procurement_policy.purpose_plans` authoritative on every schedule path.
- Canonical fattening rule: ET+TT + PPR, then Goat Pox for goats or Sheep Pox for sheep; exclude FMD, HS, Z1+Z3, and wrong-species pox.
- Restore the operator execution contract for moved vaccination work: one card per business-day
  shed/partition, and tapping that card opens the RFID scan roster even when the card combines
  multiple assignment/task identities.

## Done

- Read-only STG diagnosis: DOB 2026-09-16, ET+TT 4-week floor 2026-10-14, erroneous due date 2026-09-24.
- Confirmed deployed recovery-only floor guard is bypassed by rule-identity reconciliation.
- Reviewed the last month of vaccination generation/reconciliation commits.
- Added final generation clamping and a repository persistence guard.
- Added persistence guards to insert, deferred insert/reopen, rule-identity reconcile, manual reschedule, and drive-date override paths.
- Added the canonical fattening purpose plan to the real vaccination rule seed without changing breeding policy.
- Applied purpose-plan vaccine eligibility to birth-age, warmup/recovery, and adult schedule paths, with legacy behavior preserved for old versions that have no purpose plans.
- Focused Go tests and the isolated OCI Postgres write-path test pass.
- Preserved immutable published versions: legacy versions consume their authored top-level procurement waves, while all newly published matrices require explicit `purpose_plans.fattening`.
- Added publish-time rejection for missing/invalid fattening plans, including FMD, HS, and wrong-species pox.
- Added repository enforcement of the stored purpose plan for insert, reconcile, reopen, and reschedule callers.
- Closed the live-drive retained-date and deferred/drive-override transaction races found by the first judge pass.
- Added exact publish validation for the authored 28-day fattening second wave.
- Added explicit one-, two-, and three-vaccine applicability coverage and full goat/sheep pox selection coverage.
- Full vaccination, obligation, protocol, seed, and backend E2E suites pass after the final rule edits.
- Added age-floor enforcement to nil-reschedule recovery reopen and mapped user-facing date violations to a stable HTTP 422 contract.
- Added age-floor and purpose/species enforcement to both unchanged-rule and medically-equivalent carry-over paths.
- Added OCI regressions proving under-age rows do not carry across a republish and fattening goats carry ET+TT, PPR, and Goat Pox while excluding FMD and Sheep Pox.
- Anchored protocol second-wave pox timing to the latest actual ET+TT/PPR administration; no first-wave history means no second-wave assignment yet.
- Added trusted procurement evidence to strict carry-over floors, canonicalized spaced vaccine names, and blocked medical carry-over across incompatible trigger/repeat semantics.
- Made validation plus ordinary obligation insert one serializable transaction so concurrent anchor corrections cannot commit an invalid row between the floor read and write.
- Required the complete authored first wave before a purpose-plan second wave can be scheduled; one ET+TT-only or PPR-only history no longer unlocks pox.
- Made missing DOB, accepted-intake, and previous-completion anchors fail closed instead of silently permitting an unprovable vaccination date.
- Quarantined legacy manual fattening second-wave rows during carry-over so they regenerate through the same guarded insert path.

- Added `internal/platform/vaccinepurpose`: one purpose-plan resolver used by generation, persistence, reconcile, reschedule, drive override, and both carry-overs. non_breeding and unconfigured purposes fail closed; breeding/unspecified without an authored plan keep the full schedule; authored plans for any purpose govern.
- Second wave is generic: any governed second-wave vaccine, any trigger type, needs every first-wave vaccine plus the configured delay after the latest accepted/trusted first-wave administration. Scoped anchor events never count as first-wave doses.
- Added `obligation_instances.schedule_basis` (migration 000402, `anchored` | `anchor_missing_catch_up`). Only generation's approved adult catch-up sets the exception; the repository allows a missing DOB/arrival anchor only for that basis, a truly missing anchor, and a blank vaccine family.
- One persistence validator (`vaccination_write_guard.go`) runs inside the serializable write transaction of every path, with bounded 40001/40P01 retry; reconcile validates after its row lock against the date it writes.
- Carry-over selects and locks candidates, validates each against the new rule in Go, and rebinds valid rows in place (obligation id, batch, drive membership, completions, idempotency key preserved); invalid rows stay on the retired version. Hard-coded fattening SQL and blanket manual second-wave quarantine removed.
- Generation skips only the guard-rejected vaccine (`GuardRejected`) instead of failing the goat, and reports it in CLI, stage log, HTTP, and seed summaries.

## Pending

- Build/install the final ProdDebug APK and run device E2E for the exact Amit two-animal ET+TT
  shape before claiming the duplicate-card/scan-route fix complete.
- Run final judges against the pushed complete-first-wave, fail-closed-anchor, and Android
  execution checkpoint.
- Keep the draft PR updated with each verified checkpoint.
- Repair/read back STG after the durable guard is deployable; do not claim the DB repair durable before deployment.

## Verification

- Android/backend execution correction after judge rejection: mixed assignments now open Scan,
  the request is pinned to the card date and partition, taskless roster rows retain their own task
  write identity, and RFID/proof persistence uses that row identity. Day/card counts collapse 2/3
  vaccine rows per assignment without collapsing separate animals.
- Focused ProdDebug tests passed after that correction: `ExecutionRouteIdentityTest`,
  `ShedsExecutionIdentityTest`, `ShedsViewModelTest`, and complete `ScanViewModelTest` (97 tests),
  including taskless RFID persistence plus 1/2/3-vaccine animal-grain counts.
- Focused Go packages passed: `go test ./internal/vaccinationexecution/adapters/http
  ./internal/vaccinationexecution/adapters/postgres`; planned-date and per-row task identity are
  pinned in the scan-roster integration test.

- Android operator execution checkpoint: 48 focused ProdDebug tests passed across
  `ExecutionRouteIdentityTest`, `ShedsExecutionIdentityTest`, and `ShedsViewModelTest`, including
  two assignment rows collapsing to one two-animal card and routing to the shed/partition scan
  roster without an arbitrary task identity.
- Adjacent Android scan gates passed: complete `ScanViewModelTest` and
  `ExecutionRepositoryPaginationTest` suites, including shed-wide, task-scoped, and
  assignment-scoped Room/network roster isolation.
- Current base SHA: `5448b81ea`
- Focused tests: `go test ./internal/protocol/app ./internal/vaccination/app ./cmd/seed-vaccination-real ./internal/obligation/adapters/postgres ./internal/obligation/app ./migrations/postgres` passed.
- Full tests: `go test ./internal/vaccination/... ./internal/obligation/... ./internal/protocol/... ./internal/vaccinationexecution/... ./cmd/seed-vaccination-real ./tests/e2e -count=1` passed.
- OCI Postgres: `TestVaccinationBirthAgeFloorGuardsEveryWritePath` passed against the throwaway database in 165.15s, including nil-reschedule recovery reopen.
- OCI Postgres: carry-over collision, repeat-cause collision, valid BT dose-2 rebind, under-age rejection, and fattening purpose/species tests passed against disposable databases.
- OCI Postgres: strict trusted-history floor, incompatible trigger-transition rejection, and valid BT medical rebind all passed in 130.287s.
- OCI Postgres: valid BT rebind, trusted previous-completion floor, incompatible trigger rejection, every write-path floor, under-age carry-over, and fattening purpose/species cases passed in 128.747s after the final anchor changes.
- Final focused suite after the complete-first-wave and fail-closed-anchor changes: `go test ./internal/vaccination/... ./internal/obligation/... ./internal/protocol/... ./internal/vaccinationexecution/... ./cmd/seed-vaccination-real ./tests/e2e -count=1` passed.
- E2E/readback: STG repair and deployed repeated-sweep readback pending; no deployment from this branch.
- Judge status: latest review found partial-first-wave, manual second-wave, trusted-name normalization, and missing-anchor gaps; all are fixed and awaiting final review of the pushed SHA.
- Deployment state: not deployed

- OCI (dedicated disposable container `goatos-pr391-throwaway`, not STG): `go test ./internal/obligation/adapters/postgres -run 'TestVaccination|TestPublishing|TestRepublishing|TestCarryOver|TestStrictCarryOver|TestMedicalCarryOver|TestInFlightWork|TestReconcile|TestReschedule|TestInsertDeferred' -count=1 -timeout 40m` in 273s: all 7 new write-contract tests pass (catch-up vs ordinary missing anchor, non_breeding/unconfigured fail closed, authored breeding plan, history channels, goat/sheep second-wave carry-over with 0/partial/complete history and 1/2/3 vaccines, valid manual second-wave rebind in place, concurrent DOB/arrival/completion change, animal-set anchor chained follow-up). `TestReconcileLeavesAssignedDriveWork…` fails in fixture setup (P0001 location-scope trigger), identically at the pre-change head.
- OCI: `go test ./migrations/postgres -count=1` passed (340s).
- Go: `go test ./internal/vaccination/... ./internal/platform/vaccinepurpose ./internal/kernelstages ./cmd/seed-vaccination-real ./cmd/generate-vaccination-obligations ./tests/e2e -count=1` passed.
- OPEN: the full `internal/obligation/adapters/postgres` package has ~130 OCI failures that also fail at fd82f9cd3 (fixture goats without DOB / first follow-ups without completion rejected by fail-closed anchors, P0001 location-scope trigger, `protocol_rules_catch_up_check`, empty batch ids). Must be triaged against origin/main and fixed before landing.
- OPEN judge items: set-based carry-over validation and goat-chunked transactions (perf), lock ordering, procurement-row selection preferring accepted intake, completion path locking the goat row.
- Flake noted: `TestOperatorConfigReplanHandlerConcurrentDuplicateDeliveryIsSerializedToOneRecompute` failed once under load; 20/20 passes in isolation; package untouched by this branch.

## Known failure and before metric

- Before: one 8-day-old animal was regenerated daily onto the current date, inflating Amit's tracker from 2 to 3 administrations.
- Expected after: the obligation stays at 2026-10-14; Amit remains at 2 assigned animals/administrations; repeated generation is idempotent.

## Handoff - 2026-09-24 (account switch)

State: all work below is committed and pushed to PR 391 as a WIP checkpoint. Builders and judges were
stopped for the account switch; their last in-flight edits are included. Nothing merged, landed,
deployed; STG untouched (read-only).

Verified on this exact tree before pushing:
- `go build ./...`, `go vet` on obligation/vaccination/vaccinationexecution/kernelstages/platform: clean.
- `go test ./internal/vaccination/... ./internal/obligation/... ./internal/protocol/... ./internal/vaccinationexecution/... ./internal/platform/vaccinepurpose ./internal/platform/vaccinationanchor ./internal/kernelstages ./cmd/seed-vaccination-real ./cmd/generate-vaccination-obligations ./tests/e2e -count=1`: all pass (non-DB).
- OCI integration tests were NOT re-run on this final tree. Rerun first (see below).

In this checkpoint beyond b441234a5 (partially OCI-verified by builders, re-verify):
- `internal/platform/vaccinationanchor`: shared anchor-event semantics for generation and the persistence guard (clinical P2: guard was a superset of anchor_admins).
- Validator split into load-inputs + pure decide (`vaccination_write_decide_test.go`); set-based carry-over work and scale test `carryover_scale_integration_test.go` (P1-13/P2-17) - status: in progress when stopped.
- Migration 000401: completion acceptance locks the goat row (P2-19); canonical accepted-intake procurement-row selection (P2-18) in generation SQL and guard.
- Race tests `vaccination_write_race_integration_test.go` (P1-15): insert/reschedule/deferred reopen/carry-over passed on OCI; drive-override race was being moved to a longer timeout; the reconcile completion-race assertion in `vaccination_write_contract_integration_test.go` was being fixed to count only reconcile-written rows (pre-existing 10-09 row is lifecycle-repaired by the next pass, not a product bug).
- Fixture repairs across ~15 obligation integration test files (goats without DOB, follow-ups without prior completion) for the ~130 full-package failures; triage vs origin/main was in progress.
- GuardRejected metric (`internal/platform/kmetrics/vaccination.go`, kernelstages) and guard agreement test (`guard_agreement_integration_test.go`).

Open items (judge file kept outside git at /Users/raviteja/mesha/judge-findings-pr391.md):
- P1-13 / P2-17 set-based carry-over + goat chunking + ordered locks: finish and verify.
- P2-20 AcceptIntake must lock/update goats before procurement_load_goats.
- P2-21 in_progress carry-over candidates: rebind with retained-date validation or exclude; justify via live-drive runbook.
- P2-22 verification acceptVaccinationBatch: ordered goat locks + bounded retry.
- P2-23 drive override comment + lock order.
- Full `internal/obligation/adapters/postgres` OCI package: finish triage vs origin/main; fix branch-caused failures.
- GuardRejected persisted on vaccination_generation_runs (needs a migration) - optional.
- Before landing: rebase on origin/main (main has 000399_growth_sale_price_by_stage_sex; regenerate sqlc schema.sql copies), fresh clinical + persistence judges on the exact pushed SHA, both must sign off, then `make land-main`. Then STG repair/readback for RFID 901007000506144 after deployment.

How to run OCI tests (disposable DB, never STG): a dedicated throwaway container `goatos-pr391-throwaway` on
goatos-oci (podman, 127.0.0.1:55433). Tunnel: `ssh -f -N -L 127.0.0.1:55492:127.0.0.1:55433 goatos-oci`.
Password: `sudo podman inspect goatos-pr391-throwaway` env on the box. Set
`GOATOS_PGTEST_ADMIN_DSN=postgres://postgres:<pw>@127.0.0.1:55492/postgres?sslmode=disable GOATOS_RUN_POSTGRES_TESTS=1`.
Remove the container when the PR is done: `sudo podman rm -f goatos-pr391-throwaway`.
