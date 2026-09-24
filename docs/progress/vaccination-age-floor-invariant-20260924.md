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

## Pending

- Fix final judge finding: apply floor and purpose guards to strict unchanged-rule carry-over.
- Rerun OCI carry-over regressions and final judges.
- Keep the draft PR updated with each verified checkpoint.
- Repair/read back STG after the durable guard is deployable; do not claim the DB repair durable before deployment.

## Verification

- Current base SHA: `5448b81ea`
- Focused tests: `go test ./internal/protocol/app ./internal/vaccination/app ./cmd/seed-vaccination-real ./internal/obligation/adapters/postgres ./internal/obligation/app ./migrations/postgres` passed.
- Full tests: `go test ./internal/vaccination/... ./internal/obligation/... ./internal/protocol/... ./internal/vaccinationexecution/... ./cmd/seed-vaccination-real ./tests/e2e -count=1` passed.
- OCI Postgres: `TestVaccinationBirthAgeFloorGuardsEveryWritePath` passed against the throwaway database in 165.15s, including nil-reschedule recovery reopen.
- E2E/readback: STG repair and deployed repeated-sweep readback pending; no deployment from this branch.
- Judge status: clinical judge signed off; DB judge found one remaining strict carry-over bypass.
- Deployment state: not deployed

## Known failure and before metric

- Before: one 8-day-old animal was regenerated daily onto the current date, inflating Amit's tracker from 2 to 3 administrations.
- Expected after: the obligation stays at 2026-10-14; Amit remains at 2 assigned animals/administrations; repeated generation is idempotent.
