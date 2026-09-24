# Vaccination age-floor invariant - 2026-09-24

## Scope

- Prevent vaccination generation/reconciliation/reschedule from moving a birth-age dose before DOB + rule offset.
- Repair the real STG ET+TT obligation for RFID `901007000506144` after the code guard is ready.
- Prove repeated sweeper reconciliation cannot recreate the early row.
- Make the authored `procurement_policy.purpose_plans` authoritative on every schedule path.
- Canonical fattening rule: ET+TT + PPR, then Goat Pox for goats or Sheep Pox for sheep; exclude FMD, HS, Z1+Z3, and wrong-species pox.

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
- Added `obligation_instances.schedule_basis` (migration 000400, `anchored` | `anchor_missing_catch_up`). Only generation's approved adult catch-up sets the exception; the repository allows a missing DOB/arrival anchor only for that basis, a truly missing anchor, and a blank vaccine family.
- One persistence validator (`vaccination_write_guard.go`) runs inside the serializable write transaction of every path, with bounded 40001/40P01 retry; reconcile validates after its row lock against the date it writes.
- Carry-over selects and locks candidates, validates each against the new rule in Go, and rebinds valid rows in place (obligation id, batch, drive membership, completions, idempotency key preserved); invalid rows stay on the retired version. Hard-coded fattening SQL and blanket manual second-wave quarantine removed.
- Generation skips only the guard-rejected vaccine (`GuardRejected`) instead of failing the goat, and reports it in CLI, stage log, HTTP, and seed summaries.

## Pending

- Run final judges against the pushed complete-first-wave and fail-closed-anchor checkpoint.
- Keep the draft PR updated with each verified checkpoint.
- Repair/read back STG after the durable guard is deployable; do not claim the DB repair durable before deployment.

## Verification

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
