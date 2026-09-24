# Vaccination Card, Date Floor, and Kernel Repair

## Scope

- Collapse animals from one vaccination assignment into one Android shed card.
- Keep list-card and record-detail identity/counts consistent.
- Prevent generated/reconciled vaccination due dates from moving before rule source floors.
- Keep the staging kernel worker at one warm instance, with burst capacity to two.
- Repair and read back the affected Sagar staging obligation through lifecycle-safe writes.
- Make drive-date overrides preserve one executable assignment identity across every member obligation.
- Correct CEO calendar/live-tracker vaccine labels, animal counts, and completion counts from obligation truth.
- Preserve captured vaccination proof through processing/upload failures, with retry and Android/backend event evidence.
- Make legacy live-tracker lane resolution vaccine-aware and verify clean-slate capacity constraints.
- Add guarded OCI throwaway fixtures for one, two, and three vaccines per animal and the exact Amit two-dose shape.

## Done

- Confirmed STG deployment SHA `a67be34c07811cddacac1e2614897ceafa63d87b`.
- Confirmed Amit has one active assignment for two animals in Yashoda 3.
- Confirmed Android currently groups execution rows by per-animal task identity before batch/drive identity.
- Confirmed STG kernel is live at min 1 / max 2 after an operational override.
- Captured Poco CEO evidence showing an ET+TT title with incorrect Z1/Z3 labels, `0 / 2 Animals`, `0 of 2 sheds done`, and contradictory `1 animals in this drive`.
- Captured Infinix operator evidence showing two duplicate one-animal Yashoda 3 cards; opening either routes to a taskless read-only record screen instead of RFID scan.
- Read STG and confirmed Amit's single two-animal assignment contains member obligations from two different batches; the assignment batch has no SOP task and one member's source batch has the only SOP task.
- Confirmed the committed Terraform still restores kernel min 2 / max 2 even though the live operational override is min 1 / max 2.
- Read the real worker failure from STG logs: `1419` was a PostgreSQL log prefix, not a failed-goat count. The hourly generation transaction fails because `CancelOpenVaccinationObligationsForGoatDose` references `oi.tenant_id` after its materialized CTE omitted that column.
- Fixed the worker SQL CTE tenant projection; the real Postgres drive-membership cancellation regression passes.
- Changed execution aggregation to assignment identity instead of source batch identity, preserving distinct-animal counts when one moved assignment contains obligations from multiple source batches/tasks.
- Changed backend/Android operator-day summary lookup to canonical shed+partition identity, while retaining legacy task/batch summary lookup compatibility.
- Changed the committed STG kernel worker floor to min 1 / max 2 and aligned clean-slate/generated capacity constraints with the incremental `1..200` contract.
- Focused vaccination execution backend adapter/app tests pass after the assignment-grain change.
- Android mixed-batch/two-animal card and identity tests pass on `ProdDebug`.
- Android proof failure tests pass for Gallery preservation, same-file processing retry, second-failure original upload, crash recovery, and durable event emission.
- Added `.agents/skills/vaccination-execution-integrity/SKILL.md` with mandatory invariants, fixtures, proof gates, and promotion rules.
- Deployment-integrity guards pass for the worker tenant projection, today's assigned-drive cancellation protection, assignment-grain execution SQL, Terraform min 1/max 2, CEO obligation-truth migration, and clean-slate capacity contract.
- Integrated the calendar, Android, and backend assignment-identity implementations: calendar events now use assignment identity; Android carries assignment identity through navigation/cache/API; and the roster resolves all active assignment members across source batches.
- Combined focused backend and Android suites pass after integration.
- Replayed the disposable local phone-QA database through the PR-local vaccination operator-status migration (renumbered to `000397` in the clubbed main candidate) on `127.0.0.1:15544` and reseeded the exact widened vaccination fixture.
- Verified on physical Infinix as Amit against the throwaway backend: one Castro 1 card carries three vaccines for three animals, one Castro 2 ET+TT card carries two animals, and tapping Castro 2 opens the scan roster with `0/2` and `2 PENDING` instead of the taskless record screen.
- Verified on physical Poco as CEO against the same throwaway backend: ET+TT renders `0 / 2 Animals` and `2 animals in this drive`, ET+TT/PPR renders `0 / 6 Animals` and `6 animals in this drive`, with no Z1/Z3 leakage onto the ET+TT card.

## Pending

- Guardedly repair Sagar's obligation and verify DB plus admin-web readback.
- Run full local CI/landing receipt after final edits and before any merge/deploy.
- Optional deeper device proof still pending: end-to-end proof-video capture/failure on the physical phones. Unit and contract guards are green, but this has not yet been re-run as a fresh phone video capture in this final branch state.

## Evidence

- Browser: `/vaccination/live-tracker` loaded on STG; Yashoda 3 showed two administrations.
- Phone screenshot: two identical one-animal Yashoda 3 cards; opening either showed aggregate `0/2`.
- STG assignment `3aad3cd4-0834-41c9-96db-44e4493c6b2a` has two active members whose obligations reference batches `b10f950b-b3bd-4f59-b543-5d79b83af3d0` and `3aefb4db-b259-49ba-a821-5c7ff04f709a`; this violates the executable-card contract.
- Evidence files are under `tmp/evidence/` and were visually inspected before use.
- First Android focused-test attempt did not compile because the isolated worktree had no `local.properties`; rerun uses `ANDROID_HOME=/Users/raviteja/Library/Android/sdk`.
- First live-tracker focused-test command used a repo-root path from inside `backend` and failed before tests; rerun uses the module-relative path.
- First Android focused-test compile reached Kotlin and failed because the nullable `cardSummaries` map was used through a bound callable reference. The null-safe correction and focused rerun are green.
- `:core:core-data:testDebugUnitTest` focused proof durability suite is green: first processing failure preserves/saves the original, retry reprocesses it, repeated failure queues the existing original, and crash recovery/event rows remain idempotent.
- `go test ./internal/vaccinationexecution/adapters/postgres ./internal/obligation/adapters/postgres -run 'TestVaccinationExecutionDeploymentContracts|TestCancelGoatDoseCTEProjectsTenantUsedByDriveGuard|TestCancelGoatDoseDoesNotCancelTodayDriveAssignedVaccination' -count=1` is green.
- `go test ./internal/vaccination/app ./internal/obligation/adapters/postgres -run 'RecoveryRescheduleKeeps|DeferredRepeatCycleFinds|PublishingAnAddedVaccine|EditingOneVaccine' -count=1` is green.
- `go test ./internal/proof/app ./internal/vaccination/app ./internal/vaccination/adapters/postgres ./internal/vaccinationexecution/app -count=1` is green before the assignment-identity implementation lands; rerun is required after integration.
- Cloud Run: `goatos-kernel-worker-stg-00481-phd`, 100% traffic, operational min 1 / max 2.
- Disposable OCI migration replay reached `000393` and failed transactionally because `vaccination_drive_assignments` has no `canceled_at` column. Cancellation is member/obligation/batch lifecycle state. The invalid assignment-level predicates were removed from the migration and adjacent calendar/roster SQL before retry.
- The first OCI-backed Calendar 1/2/3-vaccine rerun failed before exercising product SQL because the code-only dimension fixture left `$4` untyped in `jsonb_build_object`; the helper now casts it explicitly to text and the exact gate must rerun.
- The second OCI-backed Calendar rerun exposed an invalid fixture shape: two unfinalized source batches used the same protocol/shed/day and violated the real uniqueness contract. The second batch is now a prior-day carry-over joined into the current assignment, matching the production carry-over model.
- The third OCI-backed Calendar rerun produced no assignment event because the fixture anchored the current-day assignment to the prior-day carry-over batch. The assignment now anchors to the current-day batch while retaining one member obligation from the prior-day source batch, matching Amit's STG topology.
- The fourth OCI-backed Calendar rerun still produced no event because the test window started one hour before the dose timestamp while assignment cards are normalized to business-day midnight. The query now starts at `biztime.BusinessDayStart(day)`, and each incremental 1/2/3-vaccine read uses a fresh repository so the 60-second list cache cannot mask newly-added lanes.
- The next rerun was blocked before product execution by an over-broad test edit that removed an unrelated integration test's repository initialization. That test setup line was restored and only the new 1/2/3-vaccine test's stale repository instance was removed.
- The corrected OCI run passed the assignment-card assertions at 1, 2, and 3 vaccines with two distinct animals. It then failed only while seeding the unrelated Z1/Z3 isolation card because pgx extended protocol rejects two SQL commands in one prepared call; the assignment and member inserts are now separate calls before the final isolation assertion reruns.
- After the Z1/Z3 isolation list passed, opening the assignment card failed in `ListDriveTargets` with `invalid input syntax for type date: ""`. Assignment/batch identities carry no date, but the shared SQL parameter is date-typed in other branches; non-date identities now bind SQL NULL instead of an empty string, with the OCI test retaining the exact click-through roster assertion.
- With nullable date binding fixed, the assignment roster was empty because the exact member join still required `assignment.planned_date = NULL`. Exact assignment-ID lookups now bypass the date predicate and exclude canceled member rows; date matching remains mandatory for park-day and legacy guessed assignment lookups.
- The phone fixture now gives ET+TT dose 2/booster its own rule-dimension row but the same canonical `vaccine_code=ET+TT` as dose 1. This guards the user contract that booster doses remain one vaccine entity while the Castro fixture still carries three distinct vaccine codes.
- The OCI test then passed assignment-card click-through for both cross-batch animals. Its final moved-date read reused the pre-move list cache and again started after assignment midnight; the move assertion now uses a fresh repository and business-day start so it tests durable event identity against live SQL.
- A sidecar seed-script syntax guard was first launched from the backend module, so it could not find the repo-root script and exited before executing any test. It is rerun from the repository root below.
- Real local CEO Calendar HTTP readback returned Amit as one assignment card, one pen, two animals, one `ET+TT` vaccine, with no Z1/Z3; Castro returned three animals and three vaccines (`BT`, `ET+TT`, `PPR`). The remaining drive-name text still used dose codes and rendered ET+TT plus its booster as two names, so logical drive naming now prefers the same canonical rule-dimension vaccine identity used by the visible labels.
- Amit's initial HTTP `403` came from querying the admin `/vaccination/execution` route, which is intentionally not operator-readable. Operator verification is continuing against the mobile `/app/vaccination/execution` and assignment-scoped roster routes used by Android.
- Runtime/auth judge confirmed `/app/bootstrap`, `/calendar/vaccination/events`, and `/app/vaccination/execution` return 200 for fresh CEO and Amit local sessions. It found a latent assignment-target scope bug when an assigned goat moves parks; the SQL now selects assignment park/shed for assignment event IDs and retains goat-location scope only for legacy events. Focused backend tests are green after updating the SQL-shape guard.
- Android judge found same-shed assignments still merged, assignment 404 falling back shed-wide, and canceled members entering cards. Assignment identity now participates in backend summary JSON and Android card keys; assignment fallback is fail-closed; canceled members are filtered. Focused Android rerun is pending because the concurrent proof-media durability implementation added a DAO method before updating a test fake.
- Proof-media judge found Gallery insert-null could falsely emit success, Gallery failures had no retry, and vaccination marked local work done despite failed processing/no upload. These are active P1 fixes; previous proof-focused green tests were insufficient and are not final sign-off evidence.
- Local API rebuild attempt after label/card fixes stopped at compile because the concurrent proof logging change passed `[]slog.Attr` to `slog.Group`; the owner is fixing that compile error before runtime HTTP readback.
- Tightened the OCI-backed Calendar assignment regression so it now asserts the exact CEO/operator symptoms: one assignment card keeps one pen/partition, vaccine labels stay canonical, unrelated Z1/Z3 stays isolated, click-through targets include every assignment member, and moved assignment dates keep stable identity. The external Postgres run passed in 122.73s.
- Runtime CEO/API readback after rebuild showed Amit's two-animal ET+TT assignment as one pen and no Z1/Z3. It also exposed a remaining same-class whole-assignment bug: Castro 1's three-vaccine assignment still rendered as three pens because assignment partition `whole` fell back to goat partition labels. The Calendar SQL now treats assignment partition as authoritative and uses goat partition only for non-assignment legacy rows; runtime readback must rerun after rebuild.
- Local phone-QA migration/reseed passed through `000393` on `127.0.0.1:15544/goatos`; seeded Amit/Pramod vaccination cases include one-, two-, and three-vaccine animals plus the exact two-animal ET+TT shape.
- Physical Infinix Amit evidence:
  - `/Users/raviteja/mesha/goatos-vaccination-card-floor-fix/tmp/e2e/final/infinix-amit-worklist-4.png`
  - `/Users/raviteja/mesha/goatos-vaccination-card-floor-fix/tmp/e2e/final/infinix-amit-castro2-roster.png`
- Physical Poco CEO evidence:
  - `/Users/raviteja/mesha/goatos-vaccination-card-floor-fix/tmp/e2e/final/poco-ceo-calendar-after-perms.png`
- The first Poco screenshot after install showed the permission gate because the test install intentionally cleared app data. After granting permissions, the same build loaded the CEO calendar correctly. A normal force update with the same package/signature does not clear runtime permissions; uninstall, package change, Android profile change, or `pm clear` does.
- Final independent judges after the physical-phone proof:
  - Backend vaccination/calendar/live-tracker judge: no P0/P1 on this worktree.
  - Android vaccination/proof-media judge: no P0/P1 on this worktree.
  - Infra guard judge: no code P0; required safety artifacts are included in the commit.
- Rebased final branch HEAD onto current `origin/main`; the test run SHA before the final doc-only amend was `8267dbe9adcbe6945a0d191cd0bcf6b4ac924747`.
- Rebased final gates:
  - `go test ./internal/calendar/adapters/postgres ./internal/calendar/domain ./internal/vaccinationexecution/adapters/postgres ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/http ./internal/obligation/adapters/postgres`
  - `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk ./gradlew :app:testProdDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.viewmodel.ShedsExecutionIdentityTest' --tests 'sg.mesha.goatos.viewmodel.ShedsViewModelTest' --tests 'sg.mesha.goatos.viewmodel.ScanViewModelTest' :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.ExecutionRepositoryPaginationTest' --tests 'sg.mesha.goatos.core.data.capture.CaptureRepositoryTest' :core:core-analytics:testDebugUnitTest --tests 'sg.mesha.goatos.core.analytics.AnalyticsContractTest'`
  - `make mobile-guard`
  - `GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_PGTEST_ADMIN_DSN='postgres://postgres@127.0.0.1:15544/postgres?sslmode=disable' go test ./internal/vaccinationexecution/adapters/postgres -run '^TestScanRosterAssignmentIncludesActiveMembersAcrossSourceBatches$' -count=1 -v`
  - `git diff --check`

## Status

- Current SHA: branch HEAD containing this progress document
- Judge status: no P0/P1 after final re-review
- Deployment state: previous STG deployment complete; this repair is not yet pushed, merged, or deployed.
# Backend roster continuation note (2026-09-23)

- A focused format/test command initially failed before compilation because it ran from `backend/` while passing `backend/...` file paths to `gofmt`. No test executed; rerun uses module-relative paths.
- Backend assignment-scoped roster implementation is complete: execution/API rows expose `assignmentId`, the roster accepts `assignment_id`, and active assignment members are authoritative across source batches while canceled assignments/members are excluded.
- Green: `go test ./internal/vaccinationexecution/... -count=1`.
- Green focused: `go test ./internal/vaccinationexecution/domain ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/http ./internal/vaccinationexecution/adapters/postgres -run 'TestScanRoster|TestVaccinationExecutionDeploymentContracts' -count=1`.
- Postgres integration fixture added for two active members from source batches A/B with only batch A owning the SOP task, plus one canceled member. Docker was unavailable, so the final run used the existing disposable local Postgres on `127.0.0.1:15544`; `GOATOS_RUN_POSTGRES_TESTS=1 ... -run '^TestScanRosterAssignmentIncludesActiveMembersAcrossSourceBatches$' -v` passed.

# Phone proof E2E continuation note (2026-09-24)

- Rebuilt and installed `devDebug` on physical Infinix serial `143382555G111292`, Android user `10`, against local throwaway Postgres/API (`127.0.0.1:15544` / app reverse to API `8081`).
- Reseeded the five-shed phone-QA fixture with one-, two-, and three-vaccine animals. Gandhi 2 carries three animals and three vaccines per animal (`PPR`, `FMD`, `HS`).
- Visual evidence:
  - `tmp/e2e-screens/operator-after-permissions-amstart.png`: worklist shows one Gandhi 2 card with `3 doses`, plus one/two-vaccine cards for adjacent sheds.
  - `tmp/e2e-screens/operator-gandhi2-scan-open.png`: tapping Gandhi 2 opens the scan screen with `0/3`, not a taskless record screen.
  - `tmp/e2e-screens/operator-gandhi2-after-raw-rfid-fixed.png`: raw duplicate-looking RFID `901007000504418` is rejected as `Unknown tag · not in this shed`; camera does not open.
  - `tmp/e2e-screens/operator-gandhi2-after-three-valid-tags.png`: three valid prefixed RFIDs complete Gandhi 2 as `3/3`, `3 DONE`, `0 PENDING`; visible proof rows show `Proof synced`.
- Device event evidence includes `proof_processing_completed`, `proof_gallery_save_started`, `proof_gallery_save_completed`, `proof_upload_registered`, `proof_upload_started`, transient `sync_write_attempt_failed` for `PROOF_UPLOAD` with `HttpException`, retry success on attempt `3/8`, `proof_upload_completed`, and per-obligation `SCAN_CAPTURE` success.
- DB readback on the throwaway database for Gandhi 2 / task `91000000-0000-4000-8000-000000000702`:
  - `obligation_instances`: 9 rows, 3 animals, 9 completed, vaccines `FMD,HS,PPR`.
  - `vaccination_completions`: 9 rows, 3 animals, 9 recorded.
  - `proof_artifacts`: 3 completed local video proofs, one per animal, each with `field_key=vaccination_goat_proof`, the prefixed `rfid_tag`, `capture_source=in_app_camera`, and all three `obligation_cycles`.
- Additional focused guards after these edits:
  - `cd backend && go test ./internal/vaccination/adapters/postgres ./internal/vaccination/app ./internal/obligation/adapters/postgres ./internal/ceoai/reporting`
  - `cd apps/goatos-android && ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew :app:testDevDebugUnitTest --no-configuration-cache --tests 'sg.mesha.goatos.rfid.DefaultRfidInputTransformTest' --tests 'sg.mesha.goatos.viewmodel.ShedsExecutionIdentityTest' --stacktrace`
  - `cd apps/goatos-android && ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew :core:core-data:testDebugUnitTest --no-configuration-cache --tests 'sg.mesha.goatos.core.data.ScanRosterRejectionBugTest' --stacktrace`
- Remaining before promotion: final diff review, `git diff --check`, final PR push, and judge reread of the pushed diff.
