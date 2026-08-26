# Vaccine Inventory Director Task Progress

Branch: `vaccine-inventory-director-task`
Base: `origin/main`
Started: 2026-08-26

## Requirement Summary

Create an `inventory_vaccine` gated PC director task seven days before a
scheduled vaccination date. The task asks the director to verify vaccine stock
availability with fridge photo or video proof. The task window is 24 hours; if
unfinished after that, it becomes overdue.

The task must be created from canonical data, not only from UI/API writes, so
manual DB changes and anchor-date generated future vaccination schedules are
covered by the same reconciliation path.

## Existing Kernel Facts

The watcher is the consolidated `kernel-worker`, not Cloud Scheduler or Cloud
Tasks.

- Google staging service: `goatos-kernel-worker-stg`
- Source: `backend/cmd/kernel-worker/main.go`
- Staging Terraform: `infra/envs/stg/cloud_run_worker.tf`
- Always-on service, two instances, CPU always allocated
- Staging has zero Scheduler jobs for kernel work
- Cloud Tasks near-term kernel queue is retired

Relevant stages:

- Continuous: Pub/Sub domain-event consumer
- 1 minute: outbox relay and notification dispatcher
- 5 minutes: obligation sweep and operational reconciliation stages
- 1 hour: vaccination generation and housekeeping/recovery

The vaccine inventory director task should plug into the kernel worker as an
idempotent reconciliation stage, most likely on the five-minute operational
lane.

Automated guardrail: `TestKernelWorkerSchedulesPcCareInventoryVaccineOnOperationalCadence`
now asserts the deployed `kernel-worker` registers
`NewPcCareInventoryVaccineStage` on the `"operational"` 5-minute cadence. This
is the code-level proof of who continuously watches direct DB/future drive data
for this feature.

## Implementation Checklist

- [x] Inspect canonical vaccination schedule, obligation, batch, and task schema.
- [x] Inspect Android role/card query and filtering paths.
- [x] Inspect CEO/progress and verifier proof-media surfaces.
- [x] Add backend/kernel reconciliation for director vaccine inventory tasks.
- [x] Add task proof and verification routing if missing.
- [x] Fix stale current-card filtering for operator/director.
- [x] Add/adjust Android UI for modern director inventory card placement.
- [x] Add backend unit/integration coverage.
- [x] Add Android unit/UI coverage where feasible.
- [x] Run focused backend and Android tests.
- [x] E2E with direct DB schedule changes.
- [x] API/automated coverage for schedule changes.
- [x] Automated coverage for anchor-date/future schedule generation.
- [x] Automated coverage for manually added animal and vaccination-rule obligation creation.
- [x] Automated coverage for procurement/source animal path and age-based vaccination rules.
- [x] Automated coverage for shifting edge cases that affect vaccination obligations/cards.
- [x] Poco phone E2E: operator.
- [x] Poco phone E2E: director.
- [x] CEO/progress E2E.
- [x] Verifier media visibility E2E.
- [x] OCI/stg parity checks.
- [x] Run independent review/judge passes before commit.
- [ ] Full live staging UI E2E after this branch is deployed.
- [ ] Full live procurement/manual-animal UI journey.

## Findings Log

- 2026-08-26: Created clean worktree from `origin/main` to avoid unrelated dirty
  changes in `the shared goatos checkout`.
- 2026-08-26: ADB sees two Poco devices:
  `F5625U031150` (`26020PC1AI`) and `dd861eff` (`22101320I`).
- 2026-08-26: Canonical source for the new task is
  `vaccination_drive_assignments` joined to `obligation_batches`. This is the
  materialized drive plan produced from generated obligations and is independent
  of UI-only events.
- 2026-08-26: Added backend migration `000211_pc_care_inventory_vaccine.sql`:
  extends PC Care category check, adds task-level proof rows, and adds frozen
  vaccine/count requirement rows.
- 2026-08-26: Added PC Care domain/API support for `inventory_vaccine`,
  `capture_mode=task_proof`, and route
  `PUT /app/pc-care/tasks/{task_id}/proofs/{slot}`.
- 2026-08-26: Added kernel stage `pc-care-inventory-vaccine` on the 5-minute
  operational lane. It creates one live task per park/shed/partition/task-date,
  assigns active `pc_director` users, and upserts requirement lines. It uses
  the natural key and stable kernel idempotency key so repeated passes/direct DB
  edits do not duplicate cards.
- 2026-08-26: Added worker-wiring regression
  `TestKernelWorkerSchedulesPcCareInventoryVaccineOnOperationalCadence` so the
  inventory watcher cannot be accidentally removed from the always-on
  `kernel-worker` operational lane or moved to a slower/non-watch path.
- 2026-08-26: Targeted backend compile/tests passed:
  `go test ./internal/pccare/...`,
  `go test ./internal/kernelstages ./internal/permissions`,
  `go test ./cmd/kernel-worker`.
- 2026-08-26: Android PC Care now supports `capture_mode=task_proof`:
  inventory requirements render on the task screen, RFID scan/roster rows are
  hidden for this mode, one stock-fridge proof is recorded against the task,
  an offline outbox op registers it through
  `PUT /app/pc-care/tasks/{task_id}/proofs/{slot}`, and submit is enabled only
  after the proof upload has a server proof id.
- 2026-08-26: Android compile passed:
  `./gradlew :app:compileStgDebugKotlin`.
- 2026-08-26: Added backend integration coverage for direct SQL vaccination
  drive assignment/obligation data feeding the reconciler. The test asserts one
  director inventory task, ET+TT/PPR requirement counts, one director assignee,
  and idempotent replay. OCI tunnel Postgres is not available here, so
  the test is compile-verified and will execute in a Postgres-enabled CI/dev
  environment.
- 2026-08-26: Widened only the task-level inventory proof validator to accept
  live-camera photo or live-camera video. Animal PC Care slot proofs remain
  live-camera video-only.
- 2026-08-26: Android task proof UI now shows explicit `Photo` and `Video`
  actions on the fridge-stock card, binds both in-app CameraX launchers on the
  PC Care task route, and stores either media kind through the same offline
  proof/outbox registration lane.
- 2026-08-26: Operator/director PC Care worklists now ask the backend for
  current-day plus open carry-over work. Old completed cards from prior days no
  longer resurface on the execution surface; completed cards remain visible on
  their current day, and monitor/planner history remains date-based.
- 2026-08-26: Focused Android unit tests passed:
  `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`.
- 2026-08-26: Touched backend tests passed:
  `go test ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`.
- 2026-08-26: Corrected the branch verification helper and docs to use the OCI
  tunnel DB path only. The helper now documents the OCI helper/env path and no
  longer writes personal home paths into its generated report.
- 2026-08-26: With the OCI tunnel open, ran
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-relpaths tools/dev/vaccine-inventory-director-verify.sh`.
  Result: all focused backend, procurement scheduling, admin-web, contract,
  migration guard, Android sync, Android worklist/route tests passed, and
  `oci_db_check=reachable`.

## Decisions

- Do not use a new Cloud Scheduler cron for this feature.
- Do not use Cloud Tasks; the queue is retired.
- Do not rely only on Pub/Sub because direct DB writes may not emit events.
- Use stable idempotency keys for task creation.
- Task grain is one `inventory_vaccine` PC Care card per park/shed/partition
  for the date seven days before the vaccination drive. Multiple vaccines for
  that drive appear as requirement lines on the same card.
- Assignment currently targets all active workforce members whose
  `primary_role_hint` is `pc_director`. This matches existing PC Care
  assignable-role and permission behavior.
- Keep CEO/monitor history date-based. The stale-card fix is scoped to assigned
  operator/director execution worklists so leadership progress/history remains
  inspectable.
- `inventory_vaccine` is generated by kernel reconciliation from drive data; the
  Android planner catalog may know the category label, but production task
  creation for this feature is the reconciler path.

## Remaining Work

- Live staging/browser proof against this exact uncommitted branch is not
  possible until the branch is deployed to the target staging services.
- The default helper intentionally keeps OCI verification non-mutating; the
  mutating direct-DB/kernel proof is documented as a controlled seed/readback/
  cleanup pass.

## Current Completion Audit

This section supersedes older historical checkpoint tables below.

| Requirement | Current status | Current evidence |
| --- | --- | --- |
| Automatic task seven days before vaccination | Proved | OCI controlled one-pass fixture created a `2026-08-26` `inventory_vaccine` task from a direct DB `2026-09-02` vaccination drive. `TestKernelWorkerSchedulesPcCareInventoryVaccineOnOperationalCadence` pins the stage to the 5-minute operational kernel lane. |
| Direct DB/manual schedules | Proved | OCI fixture inserted `vaccination_drive_assignments`, `obligation_batches`, and `obligation_instances` directly, then the fixed reconciler created the director task and requirements. |
| First-pass task completeness | Proved | Fixed `live_tasks` to include `inserted_tasks UNION existing tasks`; OCI one-pass proof read back assignee `Chandrakant`, `ET+TT = 10`, and `PPR = 25` from the same reconciler pass. `TestInventoryVaccineLiveTasksIncludesInsertedRows` guards this shape. |
| 24-hour overdue/carry-over and no old finished cards | Proved with branch tests and API evidence | Backend/Android worklist uses current-or-carry behavior; DB/list tests cover delayed carry and old completed hiding; physical/API E2E proved same-day done can show and next-day done does not carry. |
| Director app top card and proof capture | Proved | Physical Poco director evidence shows Vaccine Stock at top, fridge proof capture, submit, pending verification, and same-day Done after verifier approval; Android tests cover route, screenshot, proof, submit gate, and sync. |
| Operator exclusion | Proved | Operator token/API returned no `inventory_vaccine` rows; operator Poco menu shows no PC Care/Vaccine Stock entry. |
| CEO progress and verifier media | Proved with branch tests and API evidence | Admin-web inventory progress tests and verifier media tests pass; API/verifier E2E showed fridge proof media reaches verifier queue and approval completes the PC Care task. |
| Judge-found source/proof/admin gaps | Fixed and verified | Independent backend/API judge found manual inventory create, stale source cleanup, and write-only task proof gaps; Android/admin-web judge found admin pagination/current-carry, server task-proof refresh, and narrow proof button risks. Fixes now block planner-created `inventory_vaccine`, require stock requirement rows before submit, cancel source-less old carry-over tasks, expose `task_proofs`, fetch all admin pages with `current_or_carry`, render server task proofs on Android, and wrap proof action buttons. |
| Procurement/new animal/age-rule path | Strong automated coverage | `TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation` covers procured adult goat -> generated PPR obligation -> drive assignment -> director inventory task; upstream vaccination procurement scheduling suite passes. A full live UI procurement journey is not separately captured. |
| Shifting/partition edge cases | Proved by automated coverage | Inventory reconciler tests cover partition-grain and exact assignment-member dose counts; existing vaccination matrix shifting coverage proves shed-shift removal from old planned drives. |
| OCI/staging layer | Proved with caveat | OCI DB tunnel proof passed and STG Cloud Run inspection shows API/admin/kernel-worker services with worker stages enabled. STG is still on the base image, so live STG cannot prove this uncommitted branch. |
| Repeatable verification/hygiene | Proved | `tools/dev/vaccine-inventory-director-verify.sh` runs focused backend, procurement, admin-web, contract, migration, Android, docs/scripts hygiene, OCI reachability, and STG inspection checks with relative report paths. |

Independent review status:

- Completed two independent read-only judge passes after the wait/read tools
  became available.
- Backend/API judge findings fixed:
  - planner/API creation of `inventory_vaccine` is rejected because the category
    is kernel-owned,
  - inventory submit now requires at least one source-backed stock requirement
    row in addition to the fridge proof,
  - reconciler stale-source cleanup now covers old delayed carry-over tasks
    beyond the previous six-day window,
  - task detail/list DTOs now expose task-level `task_proofs`.
- Android/admin-web judge findings fixed:
  - CEO progress fetches all inventory task pages and asks for
    `current_or_carry`,
  - Android renders server-returned task proofs captured on another device and
    treats them as satisfying the submit gate,
  - photo/video action buttons wrap on compact screens.
- Post-fix helper run
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-judge-fixes-oci tools/dev/vaccine-inventory-director-verify.sh`
  exited `0` with `oci_db_check=reachable`.

## 2026-08-26 Resume Checkpoint: Contract and Generated Client Drift

- Re-ran contract validation:
  `npm --prefix tools/contract-validation run validate`.
  Result: `Validated 4 OpenAPI specs, 7 JSON Schemas, and 5 example payloads.`
- Re-ran app/admin/analytics TypeScript client generation:
  `npm --prefix packages/api-client run generate`.
  Result: generation exited `0`.
- Post-generation status shows only the expected app API files changed:
  - `contracts/openapi/app-api.yaml`,
  - `packages/api-client/src/generated/app-api.ts`.
- Diff scan confirms the generated contract/client drift is limited to the
  expected `inventory_vaccine`, `task_proof`, `stock_fridge_video`,
  `PCCareInventoryRequirement`, and task-proof route additions.

## 2026-08-26 Resumed Blocker Audit: Branch-Backed Phone E2E

- Rechecked Android dev wiring. The `dev` flavor is built for physical-phone
  branch QA against a branch API that is backed by the OCI tunnel DB; `adb
  reverse` maps phone loopback to that branch API, and `goatosDevBearerToken`
  bakes a short-lived HS256 dev token into the APK.
- Rechecked the branch service path. `tools/dev/android-dev-run.sh` can start a
  LaunchAgent fallback API when pointed at an explicit `DATABASE_URL`; for this
  task, use the OCI tunnel DB.
- Current DB-backed branch QA path:
  - use the OCI tunnel helper and env documented in
    `tools/dev/vaccine-inventory-director-verify.sh`,
  - run branch API/device checks against that tunnel-backed database,
  - do not rely on an unrelated developer database for this feature proof.
- Rechecked the sanctioned phone-QA path in the repo runbook. For this task, do
  not use any non-OCI DB path; use the OCI tunnel DB endpoint. A
  later resumed pass confirmed the tunnel was reachable and the consolidated
  helper passed with `oci_db_check=reachable`.
- Conclusion: branch-backed two-Poco E2E is blocked by runtime/session state,
  not app wiring. To finish it before staging deploy, use the OCI tunnel DB; to
  finish it on staging, deploy this branch backend/API/kernel and provide
  authenticated test sessions or credentials.

## 2026-08-26 E2E Progress Addendum

- Fixed the Postgres runtime bug found by the real kernel run: Postgres
  does not support `min(uuid)`, so the inventory reconciler now uses
  `min(m.user_id::text)` for the audit creator.
- Applied migration `000211_pc_care_inventory_vaccine.sql` to the OCI tunnel
  database after correcting the task proof FK to
  reference `pc_care_tasks(task_id)`.
- Seeded a direct-DB vaccination-drive fixture for tenant
  `00000000-0000-4000-8000-000000000001`: planned vaccination date
  `2026-09-02`, task date `2026-08-26`, shed `Mandela 1 / Part 7`, 10 ET+TT
  obligations and 25 PPR obligations. Seed used existing goat IDs so normal
  target FK validation stayed active.
- Ran the actual bounded kernel worker against that DB. First pass created the
  `inventory_vaccine` task. Replay pass reported
  `tasks_created=0`, `assignees_inserted=1`, `requirements_upserted=2`, proving
  idempotent catch-up/refresh behavior for direct DB writes.
- DB result after kernel replay:
  - task `994cecc6-310d-4405-8bde-66dbf2db7589`
  - category `inventory_vaccine`
  - due `2026-08-26`
  - status/work_state `open/scheduled`
  - assignee `Chandrakant`
  - requirements `ET+TT=10`, `PPR=25`
- Verified branch API, backed by the OCI tunnel DB, with Chandrakant's token:
  `GET /app/pc-care/worklist?category=inventory_vaccine&date=2026-08-26`
  returns the director task with `capture_mode=task_proof`, expected slot
  `stock_fridge_video`, and both vaccine requirement rows.
- Verified operator isolation with the operator token: the same inventory
  worklist query returns zero rows, so the operator does not see the director's
  stock task.
- Verified stale-card behavior through API: querying Chandrakant's inventory
  worklist for `2026-08-20` returned zero rows; the execution worklist is now
  current-day plus open carry-over, not old finished history.
- Added the PC Care bootstrap/nav entry for this feature:
  `pc_inventory_vaccine`, label `Vaccine Stock`, href
  `/pc/inventory-vaccine`, priority 1, and PC Care module landing href now
  points to `/pc/inventory-vaccine` so the director lands on the stock card tab
  first.
- Added Android host route `Routes.PC_INVENTORY_VACCINE` and registered it
  before the existing PC Care categories; added the nav icon key so the tab does
  not fall back to a generic module glyph.
- Device evidence:
  - `dd861eff` operator Poco installed the role-specific dev APK and hit the
    branch API. Screenshot/XML saved under
    `context/execution/vaccine-inventory-director-e2e-2026-08-26/`.
  - The operator app stayed on the permission gate even after ADB grants, so the
    worklist visual screen could not be captured from that handset in this run.
  - `F5625U031150` director Poco had an incompatible old dev signature; after
    uninstall, MIUI blocked ADB install with
    `INSTALL_FAILED_USER_RESTRICTED: Install canceled by user`. This requires
    phone-side USB-install approval and could not be bypassed from the terminal.
- State at this point in the session: live Google `goatos-stg` verification was
  blocked by non-interactive gcloud reauthentication. This was later superseded
  by browser-based gcloud auth; current state is captured in "Resumed Goal:
  Phone Install And Gcloud Unblocked". Local code/infra inspection confirmed
  staging has the consolidated `goatos-kernel-worker-stg` Cloud Run service with
  worker stages enabled in `infra/envs/stg/cloud_run_worker.tf`.
- OCI/dev DB layer was exercised through the OCI tunnel database. The
  separate OCI seed/runbook layer found in the repo is herd-signals specific;
  no distinct OCI kernel-worker deployment definition exists apart from the
  OCI tunnel DB usage and Google Cloud Run worker topology.

## 2026-08-26 Verification Commands

- `go test ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
- `./gradlew :app:compileStgDebugKotlin :core:core-designsystem:testDebugUnitTest --tests 'sg.mesha.goatos.core.designsystem.icon.MeshaIconsNavKeyTest'`
- `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
- Bounded kernel-worker run against OCI tunnel DB; expected unrelated
  calendar/feed/vaccination-generation stage failures were observed, but
  `pc-care-inventory-vaccine` completed successfully.

## 2026-08-26 Fresh Branch Verification Pass

- Backend focused suite passed again:
  `cd backend && go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`.
- Added and passed branch service-boundary coverage:
  `TestRegisterTaskProofRequiresAssigneeAndValidatesLiveCameraMedia` proves the
  task-level stock proof path requires task assignee membership, validates with
  the photo-or-video live-camera media validator, and forwards the
  `stock_fridge_video` proof params to the store.
- Verbose audit of the DB-heavy inventory tests showed they are present but
  skipped on this machine unless `GOATOS_RUN_POSTGRES_TESTS=1` is set and a
  OCI-tunnel-backed Postgres test database is available:
  `TestReconcileInventoryVaccineTasksCreatesDirectorTaskFromDriveAssignments`,
  `TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation`,
  `TestInventoryVaccineTaskUsesDriveAssignmentPartitionGrain`,
  `TestInventoryVaccineRequirementsUseExactDriveAssignmentMembers`,
  `TestInventoryVaccineChildTablesRejectTenantTaskMismatch`,
  `TestListTasksCurrentOrCarryHidesOldCompletedButKeepsCarryAndTodayDone`, and
  `TestInventoryVaccineTaskProofGatesSubmitAndFansOutToVerification`.
- Re-ran two key DB tests with `GOATOS_RUN_POSTGRES_TESTS=1`; the standard
  package harness remained separate from the direct OCI proof path:
  `TestReconcileInventoryVaccineTasksCreatesDirectorTaskFromDriveAssignments`
  and `TestInventoryVaccineTaskProofGatesSubmitAndFansOutToVerification`.
- Ran `gofmt` over touched backend Go files, then reran the backend focused
  suite successfully. `git diff --check` is clean.
- Android focused route/proof/worklist/render tests passed again:
  `cd apps/goatos-android && ./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`.
  Gradle reused cache because inputs were unchanged.
- Admin-web focused tests and typecheck passed:
  `node --test apps/admin-web/features/preventive-care-vaccination/inventory-vaccine-progress.test.mjs apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs && npm --prefix apps/admin-web run typecheck`.
- Re-ran the same Android focused suite and admin-web focused tests/typecheck
  after the documentation and Go-formatting cleanup. Android remained green from
  Gradle cache; admin-web reported 6/6 passing tests and `tsc --noEmit` clean.
- Migration duplicate-version guard passed:
  `make migration-duplicate-versions-guard`.
- API client generation was re-run through `make api-client-check`. The command
  exits non-zero because this feature branch is intentionally uncommitted and
  `packages/api-client/src/generated/app-api.ts` differs from `HEAD`; inspection
  shows only the expected app API generated changes for `inventory_vaccine`,
  `stock_fridge_video`, `task_proof`, `PCCareInventoryRequirement`, and
  `PUT /app/pc-care/tasks/{task_id}/proofs/{slot}`. No admin or analytics
  generated client drift was produced.

## 2026-08-26 Continuation Audit Addendum

- Added automated bootstrap/nav regression coverage:
  `TestPCCareModuleLandsOnVaccineStockFirst` asserts a `pc_director` gets the
  PC Care module landing href `/pc/inventory-vaccine`, with first nav item
  `pc_inventory_vaccine` / `Vaccine Stock`.
- Added automated current/carry-over worklist coverage:
  `TestListTasksCurrentOrCarryHidesOldCompletedButKeepsCarryAndTodayDone`
  asserts an old open assigned task is carried forward, a current-day completed
  task still appears for same-day feedback, and an old completed task is hidden.
- Added automated inventory proof-to-verification coverage:
  `TestInventoryVaccineTaskProofGatesSubmitAndFansOutToVerification` asserts an
  `inventory_vaccine` task cannot be submitted before a fridge-stock proof is
  registered, accepts a `stock_fridge_video` task proof without animal scan
  rows, submits to pending verification, and emits a
  `pc_care.task.pending_verification` outbox payload with category
  `inventory_vaccine`, `animal_count = 0`, and the uploaded fridge proof media
  reference for the verifier path.
- Strengthened the inventory reconciler integration test:
  `TestReconcileInventoryVaccineTasksCreatesDirectorTaskFromDriveAssignments`
  now also proves the direct-DB drive assignment does nothing six days before
  vaccination, creates the task exactly seven days before vaccination, remains
  idempotent on replay, and then rolls forward through the PC Care kernel sweep
  as a delayed carry-over card after its 24-hour due day expires.
- Strengthened Android task-proof coverage:
  `PcCareInventoryTaskProofTest` now proves both live camera photo and live
  camera video can satisfy the fridge-stock proof path and enqueue the same
  `stock_fridge_video` task-proof registration used by backend verification.
- Added app-layer verifier enqueue coverage:
  `TestPendingVerificationHandlerCarriesInventoryVaccineFridgeProof` proves the
  durable `pc_care.task.pending_verification` event for `inventory_vaccine`
  maps into the generic verification enqueue request with category, row-version
  idempotency key, zero animal count, planned date, shed/partition context, and
  the fridge proof media ref intact.
- Added verification-bridge category/item coverage:
  `TestRegisterCategoriesIncludesInventoryVaccine` proves the PC Care verifier
  registry includes `inventory_vaccine`, and
  `TestEnqueueInventoryVaccineCreatesVerifierItemWithFridgeProof` proves the
  bridge creates a generic verification item with preventive-care/PC Care route,
  inventory category, fridge media ref, PC Care task source, planned-date
  context, and director/operator metadata.
- Added Android-facing HTTP route coverage:
  `TestPutTaskProofRoutesInventoryFridgeProofToApp` proves
  `PUT /app/pc-care/tasks/{task_id}/proofs/{slot}` decodes the fridge proof
  payload and forwards task id, `stock_fridge_video`, proof ref, idempotency
  key, actor, actor type, and trace id into the app boundary.
- Added route-permission coverage:
  `TestPCCareTaskProofRouteAllowsAssignedExecutors` proves the task-proof route
  is registered as `appRegisterPCCareTaskProof`, gated by `pc_care.execute`,
  authorized for `operator` and `pc_director`, and refused for verifier/other
  director roles.
- Added OpenAPI/generated-client contract coverage:
  `contracts/openapi/app-api.yaml` now includes `inventory_vaccine` in
  `PCCareCategory`, `task_proof` in PC Care `capture_mode`,
  `stock_fridge_video` in `PCCareSlot`, optional `inventory_requirements` on
  `PCCareTask`, and
  `PUT /app/pc-care/tasks/{task_id}/proofs/{slot}` with operation id
  `appRegisterPCCareTaskProof`. Regenerated
  `packages/api-client/src/generated/app-api.ts`, so admin-web and other
  TypeScript clients see the new category, task-level proof route, stock slot,
  and inventory requirement shape. Added
  `TestOpenAPIIncludesInventoryVaccineTaskProofContract` to guard this contract
  from silently drifting back to the old animal-only category set.
- Audited and corrected the OpenAPI inventory requirement wire shape:
  the backend and Android DTOs use `vaccine_label`, `required_doses`, and
  optional `source_batch_ids`. The first OpenAPI patch used stale
  `vaccine_key`/`required_count` names; corrected the spec, regenerated the
  TypeScript client, and added `TestTaskDTOIncludesInventoryVaccineWireShape`
  so the HTTP JSON serialization and OpenAPI contract stay aligned.
- Added cross-module generated-obligation coverage:
  `TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation` uses the
  real protocol, vaccination, and obligation Postgres repositories to create a
  post-arrival adult/procured PPR rule, generate one obligation for a procured
  adult goat from its entry-date/age path, attach that generated obligation to a
  planned vaccination drive assignment, and prove the inventory reconciler
  creates the 7-day director stock task with `PPR=1`.
- Added partition/shift-adjacent inventory coverage:
  `TestInventoryVaccineTaskUsesDriveAssignmentPartitionGrain` proves a
  partitioned vaccination drive assignment, such as the row produced after
  shift/planning machinery moves animals into a pen, creates the director
  inventory card at the same `Part 7` grain with the matching `PPR=4`
  requirement instead of collapsing back to the whole shed.
- Added CEO/monitor progress coverage:
  `TestCEOCanMonitorInventoryVaccineTasksTenantWide` proves a CEO actor with a
  tenant grant reads `inventory_vaccine` through the monitor list, not the
  assigned current/carry worklist, and receives pending-verification status plus
  stock requirements. The reconciler integration test now also asserts the
  assignee-free monitor list returns the director task with ET+TT/PPR
  requirements.
- Re-ran focused verification after the added tests:
  - `go test ./internal/pccare/app ./internal/pccare/adapters/postgres`
  - `go test ./internal/pccare/adapters/postgres`
  - `go test ./internal/kernelstages ./cmd/kernel-worker`
  - `go test ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
  - `go test ./internal/pccare/adapters/http`
  - `./gradlew :app:compileStgDebugKotlin :core:core-designsystem:testDebugUnitTest --tests 'sg.mesha.goatos.core.designsystem.icon.MeshaIconsNavKeyTest'`
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
- Re-ran the lower-level postgres package after adding the verifier fanout
  regression:
  - `go test ./internal/pccare/adapters/postgres`
- Re-ran contract/frontend verification after the OpenAPI/client patch:
  - `make api-client-generate`
  - `make api-client-check` regenerated the expected app client diff and then
    failed at the final `git diff --exit-code -- packages/api-client/src/generated`
    freshness gate because this branch intentionally has uncommitted generated
    client changes.
  - `npm --prefix apps/admin-web ci --no-audit --no-fund` completed with Node
    engine warnings (`apps/admin-web` declares Node 24.x; shell has Node 23.1.0).
  - `npm --prefix apps/admin-web run typecheck` passed.
- Re-ran verification after correcting the inventory requirement field names:
  - `go test ./internal/pccare/adapters/http`
  - `go test ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
  - `npm --prefix apps/admin-web run typecheck`
- Ran deployability/review guardrails:
  - `make validate-migrations` is still red because the repo's migration guard
    reports pre-existing historical/hot-table violations in older migrations
    such as `000022`, `000031`, `000107`, `000169`, `000170`, `000172`,
    `000197`, `000199`, and `000209`; the new
    `000211_pc_care_inventory_vaccine.sql` was not listed among the violations.
  - `make api-client-check` regenerates cleanly, then fails at its final
    `git diff --exit-code -- packages/api-client/src/generated` step because
    this branch intentionally changes `packages/api-client/src/generated/app-api.ts`.
  - Reviewed the new reconciler, task-proof, task serialization, and submit
    paths directly. Capture writes use the existing `lockTaskForCapture` gate,
    so pending/closed/canceled tasks cannot accept a new fridge proof. Submit
    uses task-proof media refs only for `inventory_vaccine`; animal tasks still
    use the existing per-animal media composition.
  - Re-ran focused tests uncached after review/comment updates:
    `go test -count=1 ./internal/pccare/adapters/http ./internal/pccare/app ./internal/pccare/adapters/postgres ./internal/pccare/adapters/verificationbridge`
    and
    `go test -count=1 ./internal/workforce/app ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`;
    both passed. `npm --prefix apps/admin-web run typecheck` also passed.
- State at this point in the session: the goal was not fully closed because
  director install approval and gcloud auth still needed external action. This
  was later superseded: first-Poco install and gcloud inspection now work, while
  authenticated sessions and a branch-backed backend are still missing.

## 2026-08-26 Device E2E Recheck

- ADB still sees both Poco phones:
  - `F5625U031150` (`26020PC1AI`)
  - `dd861eff` (`22101320I`)
- Re-attempted installing the current dev debug APK on director Poco
  `F5625U031150`; MIUI/HyperOS still returned
  `INSTALL_FAILED_USER_RESTRICTED: Install canceled by user`, so director-screen
  PC Care inventory E2E still needs phone-side USB install approval.
- Operator Poco `dd861eff` has `sg.mesha.goatos.dev` and `sg.mesha.goatos.stg`
  installed. It was initially still on the app's permission gate. ADB grants
  confirmed runtime permissions are now granted for camera, microphone,
  notifications, fine/coarse location, and Bluetooth scan/connect.
- After force-stop/relaunch, operator Poco moved past the permission gate and
  rendered the app. Fresh screenshots/XML captured:
  - `operator-after-force-stop-grants-052523.png`
  - `operator-after-force-stop-grants.xml`
- Attempted debug navigation broadcast to `/pc/inventory-vaccine`; broadcast
  returned result `0`, but the active app stayed on Vaccination sheds with
  `"Couldn't load vaccination drives. Pull to refresh or try again."` Fresh
  screenshots/XML captured:
  - `operator-pc-inventory-route-052539.png`
  - `operator-pc-inventory-route.xml`
  - `operator-pc-inventory-after-relaunch-052553.png`
  - `operator-pc-inventory-after-relaunch.xml`
- Opened the operator app navigation menu and captured:
  - `operator-modules-menu-052641.png`
  - `operator-modules-menu.xml`
- Parsed the operator menu XML. Visible modules/content descriptions are:
  `Feed`, `Herd Operations`, `Milk`, `Preventive Care`, `Vaccination`,
  `Weighing`, plus account/sign-out controls. There is no `PC Care`,
  `Vaccine Stock`, or `pc_inventory_vaccine` navigation entry in this logged-in
  operator session. This matches the backend/API expectation that the inventory
  vaccine task is director-assigned and not visible to the operator worklist.
- Net device status: operator device is no longer blocked by runtime permission
  grant state, but the requested PC Care inventory operator/director visual E2E
  is still not complete. Director remains blocked by install policy; operator
  session confirms no PC Care/Vaccine Stock surface is granted, so no operator
  PC Care inventory screenshot can honestly be claimed from this login.

## Historical Requirement Coverage Report (Superseded)

This table is retained as historical checkpoint evidence only. The authoritative
current state is the Current Completion Audit near the top of this document and
the later Resume Checkpoint: Independent Judge Fixes section below.

Status key:

- `Proved`: covered by code plus focused automated or DB/API evidence.
- `Partially proved`: implementation exists and some evidence exists, but live
  E2E proof is still missing.
- `Blocked external`: cannot be proven further from this terminal without a
  device/account approval.

| Requirement | Status | Evidence | Remaining gap |
| --- | --- | --- | --- |
| Create a PC director task for vaccine inventory stock check seven days before vaccination | Proved branch-side | `ReconcileInventoryVaccineTasks` in `backend/internal/pccare/adapters/postgres/inventory_vaccine_reconciler.go`; OCI tunnel DB evidence showed the kernel path created the director task; `TestKernelWorkerSchedulesPcCareInventoryVaccineOnOperationalCadence` proves the reconciler is registered in the always-on 5-minute operational kernel lane; latest consolidated helper passed with OCI DB reachable. | Live staging is still not deployed with this branch. |
| Trigger from direct DB changes, not only UI/API writes | Proved branch-side | OCI tunnel DB seed inserted `vaccination_drive_assignments`, `obligation_batches`, and `obligation_instances` directly; automated postgres tests seed the same tables directly, and worker-wiring regression pins the direct-DB watcher to the consolidated `kernel-worker`. | Live `goatos-stg` is reachable by gcloud but not deployed with this branch. |
| Trigger for anchor-date/future vaccination schedules | Proved branch-side | Reconciler consumes the canonical materialized future drive plan (`vaccination_drive_assignments`) rather than planner events. Focused backend coverage verifies seven-day reconciliation against future drive data and the procured-adult generation path once a future drive exists. | Full anchor-date UI/API generator-to-drive-to-inventory E2E still waits for branch-backed staging. |
| Task is 24 hours and becomes overdue/carry-over if unfinished | Proved branch-side | Inventory reconciler creates due date equal to task date; DB coverage runs `SweepTaskRollForward` and asserts `delayed` next-day carry-over; Android worklist tests prove the execution surface requests current/carry-over work. | Live branch-backed UI proof remains separate. |
| Stock requirements show vaccine labels/counts such as ET+TT and PPR | Proved branch-side | OCI DB/API evidence showed `ET+TT=10`, `PPR=25`; postgres tests assert ET+TT and PPR requirement counts. | Live staging proof waits for branch-backed deploy. |
| Director task proof accepts fridge photo or video | Proved | Backend validator accepts task-proof photo/video; Android `PcCareInventoryTaskProofTest` proves both live camera photo and video enqueue the `stock_fridge_video` task-proof registration. `TestPutTaskProofRoutesInventoryFridgeProofToApp` proves the public HTTP route forwards that task-proof registration into the app layer. `TestPCCareTaskProofRouteAllowsAssignedExecutors` proves auth exposes the route to operator/pc_director execution roles. | Live phone capture visual still needs authenticated branch-backed director session. |
| Task proof gates submit and sends media to verifier | Proved branch-side | `TestRegisterTaskProofRequiresAssigneeAndValidatesLiveCameraMedia`, `TestPendingVerificationHandlerCarriesInventoryVaccineFridgeProof`, `TestRegisterCategoriesIncludesInventoryVaccine`, and `TestEnqueueInventoryVaccineCreatesVerifierItemWithFridgeProof` prove the branch service/event/bridge contracts for fridge media. DB coverage blocks submit before proof and asserts verifier outbox payload. | Live verifier UI E2E still waits for branch-backed staging. |
| Director sees inventory card on website/app at top | Proved branch-side | Bootstrap/nav code sets PC Care landing to `/pc/inventory-vaccine` and first item `pc_inventory_vaccine`; `TestPCCareModuleLandsOnVaccineStockFirst` asserts this for `pc_director`; Android route registered before other PC Care categories; physical phone evidence captured the director top-card path against the branch API. | Live website screenshot still waits for branch-backed staging and an authenticated browser session. |
| Operator/director execution surfaces show only current or carry-over cards, not old finished work | Proved branch-side | Backend worklist `CurrentOrCarry` query and Android execution worklist use it; Android unit tests prove the execution client requests current/carry-over; DB list coverage proves old completed hidden, old open carried, and current-day completed visible. | Live staging stale-card proof waits for branch-backed deploy. |
| Operator should not see director inventory task | Proved | Branch API query using operator token returned zero rows for `category=inventory_vaccine`; director token returned the task. Operator Poco now passes the permission gate, and its module menu shows no `PC Care`/`Vaccine Stock` entry, only Feed/Herd/Milk/Preventive Care/Vaccination/Weighing. | None for operator exclusion; director visual proof remains separate. |
| CEO can see progress of director task | Proved branch-side / live UI pending | `TestCEOCanMonitorInventoryVaccineTasksTenantWide` proves CEO tenant-wide access to the `inventory_vaccine` monitor list without assignee/current-carry filtering, including pending-verification status and stock requirements. The reconciler integration test also asserts the assignee-free monitor list returns the director task with ET+TT/PPR requirements. Admin-web progress component tests cover the website surface. | Dedicated live CEO website/app visual E2E waits for branch-backed staging. |
| New manual animal/procurement/source paths create vaccination obligations by age/rules | Proved branch-side | `TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation` seeds a procured adult goat, uses a post-arrival PPR rule through the real vaccination generation service, creates the generated obligation, and proves the downstream drive assignment creates the director inventory card. | Manual animal UI/procurement-source E2E and broader rule matrix wait for branch-backed staging. |
| Shifting edge cases that affect vaccination obligations/cards | Proved branch-side | Existing obligation-side coverage includes `TestMatrixShedShiftRemovesAnimalFromOldShedPlannedDrive`; PC Care DB coverage proves the inventory reconciler follows partitioned drive assignments and exact assignment members for split partitions. | Full end-to-end shifting UI/event-to-replanned-drive-to-inventory-card journey waits for branch-backed staging. |
| OCI/stg parity | Partially proved / Blocked external | OCI tunnel DB was migrated and exercised; gcloud now confirms Google staging kernel worker service exists and has worker stages enabled. | Staging is deployed at branch base `a5a64b7bbff4`, not this feature branch; no distinct OCI kernel-worker deploy definition found. |
| Two-Poco E2E with screenshots | Partially attempted / Blocked external | Both Poco devices launch `sg.mesha.goatos.stg`; `F5625U031150` has local stg debug `0.1.33-stg` installed and `dd861eff` has installed stg `0.1.31-stg`. Saved login screenshots/XML exist under `context/execution/vaccine-inventory-director-e2e-2026-08-26/`. | Both devices remain at unauthenticated staging login, and staging backend does not contain this branch; authenticated CEO/director/operator role-flow screenshots still need credentials/session plus branch-backed backend. |
| Independent judge/review before commit | Superseded | No commit had been made at this historical checkpoint. | Later independent backend/API and Android/admin-web judge passes found gaps; those fixes were implemented and verified in the Resume Checkpoint: Independent Judge Fixes section below. |

## 2026-08-26 Judge Fixes And Fresh Verification

- Closed the two independent judge agents after integrating their findings.
- Backend judge findings fixed:
  - Inventory requirements now count the exact
    `vaccination_drive_assignment_members` for each drive assignment when member
    rows exist, so split drive batches do not overcount stock on every director
    card.
  - `pc_care_task_proofs` and
    `pc_care_task_inventory_requirements` now enforce `(tenant_id, task_id)`
    parity through a parent unique constraint and composite foreign keys, so
    direct DB inserts cannot attach child rows to another tenant's task.
  - Added regression coverage for split assignment member counts and child-table
    tenant/task mismatch rejection.
- Android/client judge findings fixed:
  - `PC_CARE_TASK_PROOF_REGISTER` now uses a task-proof-specific post-success
    refresh hook and decodes `PcCareTaskProofRegisterPayload`, instead of trying
    to decode the slot-register payload shape.
  - The ViewModel orphan-proof repair path now distinguishes animal slot keys
    from task proof keys, so `stock_fridge_video` can re-enqueue a missing
    task-proof registration.
  - Added unit coverage for the task-proof refresh hook and orphaned
    `stock_fridge_video` re-enqueue repair.
- Fresh verification after those fixes:
  - `go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
    passed.
  - `npm --prefix apps/admin-web run typecheck` passed.
  - `./gradlew :app:compileStgDebugKotlin :core:core-designsystem:testDebugUnitTest --tests 'sg.mesha.goatos.core.designsystem.icon.MeshaIconsNavKeyTest'`
    passed.
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - `make validate-migrations` still fails on pre-existing historical hot-table
    migration guard violations, but the new
    `000211_pc_care_inventory_vaccine.sql` migration was not listed in the
    violation output after the composite-FK change.
- Remaining external E2E gaps are unchanged:
  - Director Poco `F5625U031150` still needs phone-side USB install approval
    before the director inventory card can be screenshot on device.
  - Live `goatos-stg` inspection still needs interactive `gcloud`
    reauthentication.
  - CEO/director visual progress screenshots are still pending those live access
    blockers.

## 2026-08-26 Continuation Recheck

- Rechecked current branch/worktree before relying on prior context:
  - Branch remains `vaccine-inventory-director-task`.
  - Implementation files and this progress document are still uncommitted.
- Rechecked both connected Poco phones:
  - Director Poco `F5625U031150` is connected, but still has no Goatos package
    installed. Retrying
    `adb -s F5625U031150 install -r apps/goatos-android/app/build/outputs/apk/dev/debug/app-dev-debug.apk`
    again returned
    `INSTALL_FAILED_USER_RESTRICTED: Install canceled by user`.
  - Captured latest director blocked-state screenshot:
    `context/execution/vaccine-inventory-director-e2e-2026-08-26/director-install-blocked-latest.png`.
  - Operator Poco `dd861eff` is connected and focused on
    `sg.mesha.goatos.dev/sg.mesha.goatos.MainActivity`.
  - Captured latest operator screenshot/XML:
    `operator-current-latest.png`
    and `operator-current-latest.xml`.
  - Latest operator XML again shows user `Amit Kumar`, role `operator`, and only
    these modules: `Vaccination`, `Weighing`, `Herd Operations`, `Feed`, `Milk`,
    and `Preventive Care`. There is still no `PC Care`, `Vaccine Stock`, or
    inventory-vaccine navigation surface in this operator session.
- Rechecked staging access:
  - `gcloud config list` still reports project `goatos-stg` and account
    `<active-gcloud-account>`.
  - `gcloud run services describe goatos-kernel-worker-stg --region=asia-south1`
    still fails with non-interactive reauthentication:
    `Reauthentication failed. cannot prompt during non-interactive execution`.
  - Local authoritative deployment files still show staging uses Cloud Run
    service `goatos-kernel-worker-stg`, command `/app/bin/kernel-worker`,
    args `-timeout=0s`, min/max instances `2`, CPU always allocated, and
    `GOATOS_WORKER_STAGES_ENABLED=true`.
  - Repo search did not find a separate OCI kernel-worker deployment definition;
    OCI references in this tree are seed/runbook/data-access oriented, while
    kernel-worker runtime deployment is defined for dev/stg Cloud Run.
- Added stronger offline deploy/runtime guard evidence:
  - `make deployed-job-flags-guard` passed, proving deployed Cloud Run job
    commands/args still match `deploy/runtime/workers.json` and resolve to
    built binaries with defined flags across the checked environments.
  - `node tools/agent-hooks/check-worker-stage-budgets.mjs` passed, proving
    kernel-worker batch limits and cadence budgets are compatible for both
    environments.
  - `go test -count=1 ./internal/kernelstages ./cmd/kernel-worker` passed again
    after the recheck.
- Completion status is still not claimable because the requested live
  director/CEO visual E2E is not authenticated against a branch-backed backend.
  This note supersedes the earlier install/gcloud blocker wording below: gcloud
  inspection and first-Poco installs were later unblocked, but login/session and
  branch deployment are still missing.

## 2026-08-26 Android Worklist Date-Window Coverage

- Added Android ViewModel coverage for the execution worklist date clamp:
  - `PcCareWorklistDateWindowTest` proves the PC Care execution worklist refuses
    a past business date, so an operator/director cannot navigate the execution
    surface back to old finished cards.
  - The same test keeps today selectable, preserving the required behavior that
    a task completed today can still show as `Done` on the current-day card.
  - The same test allows the seven-day future window, preserving the vaccine
    inventory workflow where the director card is created seven days before the
    vaccination date.
- Extended `FakePcCareRepository` to record worklist queries and emit optional
  deterministic worklist rows while keeping the previous empty default for older
  tests.
- Re-ran focused verification:
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest'`
    passed.
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - `go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
    passed again after the Android fake/test change.
- This strengthens the `Operator/director execution surfaces show only current
  or carry-over cards, not old finished work` requirement on the Android
  execution surface. It does not remove the live director/CEO visual E2E blocker.

## 2026-08-26 Inventory Task UI Render Coverage

- Added a Paparazzi-backed Android render test for the director inventory task
  screen:
  - `PcCareInventoryTaskScreenshotTest` renders the real `PcCareTaskScreen` on a
    Pixel 6 viewport in `inventory_vaccine` / task-proof mode.
  - The sample includes ET+TT `10 doses`, PPR `25 doses`, and a deliberately long
    vaccine label (`Enterotoxaemia + tetanus booster reserve stock`) with
    `125 doses`.
  - The same render includes the `stock_fridge_video` proof slot with the photo
    and video controls enabled, plus the blocked submit reason until proof is
    captured.
- Re-ran focused verification:
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest'`
    passed.
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - `npm --prefix apps/admin-web run typecheck` passed again.
- This does not replace live Poco screenshots, but it adds automated phone-sized
  Compose rendering evidence for the exact director inventory stock card/detail
  layout that the blocked director device cannot currently display.

## 2026-08-26 Admin-Web Verifier Media Coverage

- Added verifier contract coverage for PC Care inventory proof review:
  - `pc-care-inventory-proof.test.mjs` proves `inventory_vaccine` review items
    use the generic `/verify` drawer media path instead of needing a dedicated
    PC Care verifier page.
  - The test pins that proof evidence is driven from `VerificationQueueItem.media`
    and that fridge proof videos render through the generic video player.
  - The same test pins the generic image proof branch and ensures image proof
    previews use the proof media download URL.
  - It also confirms category/action labels remain backend-owned via
    `actionTypeLabels[item.category]`, so `inventory_vaccine` is not hidden,
    remapped, or stripped of media in admin-web.
- Re-ran focused verification:
  - `node --test apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs`
    passed.
  - `npm --prefix apps/admin-web run typecheck` passed.
- This covers the CEO/verifier web media plumbing at contract level. It does not
  remove the remaining live CEO visual E2E blocker from non-interactive staging
  access and the blocked director phone install.

## 2026-08-26 Current Combined Verification Pass

- Re-ran the current targeted suite after the admin-web verifier coverage was
  added:
  - `go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
    passed.
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - `node --test apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs && npm --prefix apps/admin-web run typecheck`
    passed.
- Current local automated coverage is green for:
  - automatic inventory-vaccine task reconciliation,
  - kernel-worker stage wiring,
  - task proof upload/read contract,
  - verification fanout/media contract,
  - Android task proof outbox/sync/orphan repair,
  - Android current/carry-over worklist date clamp,
  - Android phone-sized inventory task rendering.
- Remaining unverified live surfaces, updated after the later browser-auth and
  install retry:
  - both Poco devices launch staging but remain at unauthenticated login,
  - live `goatos-stg` Cloud Run inspection works, but staging is deployed at
    branch base `a5a64b7bbff4`,
  - CEO/director visual E2E screenshots require authenticated sessions and a
    branch-backed backend.

## 2026-08-26 Direct-DB Requirement Refresh Edge Case

- Tightened the inventory reconciler for direct DB edits after a task already
  exists:
  - `ReconcileInventoryVaccineTasks` now deletes stale
    `pc_care_task_inventory_requirements` rows for each live inventory task
    when the underlying drive/obligation data no longer produces that vaccine
    label.
  - This prevents an old ET+TT or PPR stock requirement from staying on the
    director card after someone directly cancels/removes those source
    obligations in the DB and the kernel watcher reruns.
- Strengthened `TestReconcileInventoryVaccineTasksCreatesDirectorTaskFromDriveAssignments`:
  - first creates a card with ET+TT and PPR requirements,
  - then simulates a direct DB edit by canceling the ET+TT obligations,
  - reruns the reconciler,
  - asserts the same task is not duplicated and the stale ET+TT requirement is
    deleted while PPR remains.
- Re-ran verification:
  - `go test -count=1 ./internal/pccare/adapters/postgres` passed.
  - `go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
    passed.
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - `node --test apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs && npm --prefix apps/admin-web run typecheck`
    passed.

## 2026-08-26 Android Backend-Href Route Guard

- Added an Android route contract guard for the director's first PC Care card:
  - `ExecutionRouteIdentityTest` now asserts `Routes.PC_INVENTORY_VACCINE` is
    exactly `/pc/inventory-vaccine`.
  - The same test reads `AppNavHost.kt` and pins that this backend bootstrap
    href is bound to `pcCareCategoryComposable(..., "inventory_vaccine",
    "Vaccine Stock", ...)`.
- This strengthens the `director sees inventory card on top` requirement by
  guarding the handoff between backend bootstrap nav and the Android route graph.
  It is still not a substitute for the blocked director Poco screenshot, but it
  proves the shipped Android graph can resolve the backend's top PC Care href to
  the inventory-vaccine worklist.
- Re-ran verification:
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest'`
    passed.
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - `go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
    passed.
  - `node --test apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs && npm --prefix apps/admin-web run typecheck`
    passed.

## 2026-08-26 Second Independent Judge Pass

- Ran two independent judge agents before any commit:
  - backend/data/kernel judge,
  - Android/admin-web judge.
- Backend judge found two P1 gaps in the inventory-vaccine reconciler:
  - exact T-7 matching could miss direct DB/manual schedules inserted late or
    schedules created after kernel downtime,
  - if all source drive/obligation rows disappeared, stale open inventory tasks
    and requirements could remain visible.
- Fixed both backend P1s:
  - `ReconcileInventoryVaccineTasks` now scans a catch-up window
    `planned_date > as_of_date AND planned_date <= as_of_date + 7 days`, while
    still keying the task to the canonical task date `planned_date - 7 days` so
    late inserts do not create duplicate cards.
  - The reconciler now separates `source_tasks`, `candidate_tasks`, and
    `live_tasks`, then cancels stale open inventory tasks when the source drive
    no longer exists in the catch-up window.
  - It also cancels open inventory tasks with no remaining live requirement
    rows, and deletes requirement rows tied to canceled stale/empty tasks.
- Strengthened the Postgres integration test:
  - eight days before the vaccine date creates no task,
  - six days before catches up and creates the task with canonical planned/due
    date seven days before the vaccine date,
  - replay on the canonical day and late replay do not duplicate the task,
  - removing one source vaccine deletes only that stale requirement,
  - sweeping the 24-hour card carries it forward as `delayed`,
  - removing all source vaccines cancels/hides the inventory task and clears
    requirements.
- Android/admin-web judge found:
  - a carry-over visibility concern for delayed tasks,
  - missing admin-web CEO/progress UI for the PC Care inventory task,
  - remaining live phone screenshot coverage blocked by device/install access.
- Addressed the carry-over concern with an explicit Android unit test:
  - `PcCareWorklistDateWindowTest` now proves the execution worklist uses
    today's current-or-carry backend query, so a delayed inventory task planned
    yesterday and due today still appears in the current-day director worklist
    and maps to `Delayed`.
- Remaining real gap at that point:
  - admin-web had verifier media drawer contract coverage for `inventory_vaccine`,
    and backend CEO monitor API coverage existed, but a dedicated CEO/progress UI
    surface for PC Care inventory tasks was not yet implemented/proved.
- Re-ran verification after the second judge fixes:
  - `go test -count=1 ./internal/pccare/adapters/postgres` passed.
  - `go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
    passed.
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - `node --test apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs && npm --prefix apps/admin-web run typecheck`
    passed.

## 2026-08-26 Admin-Web CEO Inventory Progress Surface

- Closed the admin-web CEO/progress UI gap found by the Android/admin-web judge:
  - added `listPCCareTasks` in `apps/admin-web/lib/api/server.ts`, using the
    existing `/app/pc-care/tasks` monitor endpoint instead of creating a new
    backend read model,
  - added `InventoryVaccineProgressSection` on `/vaccination`,
  - the section filters `category=inventory_vaccine` for today's Asia/Kolkata
    business date and the selected farm scope,
  - it renders current/carry-over task counts, overdue (`delayed`) count,
    verifier-review count, completed count, director assignee names, farm/shed,
    due date, state/status chips, and vaccine dose requirement lines such as
    `10 ET+TT · 25 PPR`,
  - it filters visible rows to `scheduled`, `delayed`, and `completed`, so old
    `closed` and `canceled` work does not appear in the CEO progress section.
- Added admin-web contract coverage:
  - `inventory-vaccine-progress.test.mjs` proves `/vaccination` mounts the PC
    Care inventory progress section,
  - proves the section is server-backed by `/app/pc-care/tasks`,
  - proves it requests `inventory_vaccine`,
  - proves old closed/canceled work is filtered,
  - proves director, verifier-review, and vaccine dose evidence fields are
    rendered from the task payload.
- Re-ran verification:
  - `node --test apps/admin-web/features/preventive-care-vaccination/inventory-vaccine-progress.test.mjs apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs`
    passed.
  - `npm --prefix apps/admin-web run typecheck` passed.
- Remaining live blockers after this change:
  - director Poco install is still blocked by phone-side
    `INSTALL_FAILED_USER_RESTRICTED`,
  - live `goatos-stg` inspection remains blocked by interactive `gcloud`
    reauthentication,
  - CEO/director/verifier visual screenshots on real staging still require those
    blockers to clear.

## 2026-08-26 Final Focused Verification Snapshot

- Re-ran the focused lanes from the correct project roots:
  - from `backend/`:
    `go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
    passed.
  - from `apps/goatos-android/`:
    `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - from repo root:
    `node --test apps/admin-web/features/preventive-care-vaccination/inventory-vaccine-progress.test.mjs apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs && npm --prefix apps/admin-web run typecheck`
    passed.
- A first combined command from repo root failed before running Go because this
  branch root is not a Go module (`go.mod` is under `backend/`). The corrected
  split commands above are the authoritative verification result.

## 2026-08-26 Live Blocker Retry And Final Report Draft

- Re-checked devices:
  - `adb devices -l` shows both Poco devices connected:
    `F5625U031150` and `dd861eff`.
  - `dd861eff` has `sg.mesha.goatos.stg` installed at version `0.1.31-stg`
    / versionCode `32`; launching reaches the staging login screen.
  - `F5625U031150` still rejects APK install:
    `INSTALL_FAILED_USER_RESTRICTED: Install canceled by user`.
  - Saved fresh blocker artifacts:
    `director-install-blocked-retry.xml` and
    `director-install-blocked-retry-*.png`.
  - Saved fresh staging-login artifacts for the second Poco:
    `stg-poco-launch.xml` and `stg-poco-launch-*.png`.
- Re-checked staging deployment access:
  - `gcloud auth list` has active `<active-gcloud-account>` and project `goatos-stg`.
  - `gcloud run services list --project goatos-stg --platform managed` still
    fails with non-interactive reauthentication:
    `Reauthentication failed. cannot prompt during non-interactive execution.`
- Added the standalone detailed coverage report:
  - `context/execution/vaccine-inventory-director-final-coverage-report.md`.
  - It maps requirements to evidence, including direct DB catch-up, 24-hour
    delayed carry-over, stale source deletion, generated procured-adult
    obligation path, partition/shifting-style grain, exact assignment-member
    requirement counts, CEO progress UI, Android route/proof behavior, verifier
    media, and remaining live blockers.
- Re-ran the owning Postgres package after documenting the broader coverage:
  - from `backend/`, `go test -count=1 ./internal/pccare/adapters/postgres`
    passed.

## 2026-08-26 Branch Base And Release-Readiness Audit

- Confirmed current branch is still cut directly from `origin/main`:
  - branch: `vaccine-inventory-director-task`,
  - `git rev-parse HEAD` =
    `a5a64b7bbff4a0fc3d6f04c7760f5be4ef65ab2b`,
  - `git merge-base HEAD origin/main` =
    `a5a64b7bbff4a0fc3d6f04c7760f5be4ef65ab2b`.
- Migration checks:
  - `make migration-duplicate-versions-guard` passed.
  - `make validate-hot-index-migrations` failed on historical/pre-existing
    migration debt; the violation list did not include
    `000211_pc_care_inventory_vaccine.sql`.
  - Later OCI checks confirmed the tunnel DB path is reachable and the branch
    migration objects exist there.
- Re-ran focused verification:
  - from `backend/`,
    `go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker`
    passed.
  - from `apps/goatos-android/`,
    `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
  - from repo root,
    `node --test apps/admin-web/features/preventive-care-vaccination/inventory-vaccine-progress.test.mjs apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs && npm --prefix apps/admin-web run typecheck`
    passed.
- Updated the final coverage report with branch-base, migration-guard, and
  OCI tunnel/migration-replay status.

## 2026-08-26 Superseded Blocker Recheck

- Earlier in the session, before browser-based gcloud auth and first-Poco install
  were unblocked, the live-only blockers were:
  - both Poco devices still appear in `adb devices -l`,
  - `F5625U031150` still rejects APK install with
    `INSTALL_FAILED_USER_RESTRICTED: Install canceled by user`,
  - `dd861eff` still launches `sg.mesha.goatos.stg` to the unauthenticated login
  screen,
  - `gcloud run services list --project goatos-stg --platform managed` still
    fails with non-interactive reauthentication.
- This section is retained for timeline traceability only. The later "Resumed
  Goal: Phone Install And Gcloud Unblocked" section is the current state:
  gcloud inspection works and first-Poco install works, but both phones remain at
  unauthenticated staging login and staging is still deployed at the branch base.

## 2026-08-26 Resumed Goal: Phone Install And Gcloud Unblocked

- User asked to resume the goal and explicitly allowed browser-based gcloud auth.
- Phone/device state changed:
  - `F5625U031150` now accepts APK installs.
  - Installed the existing dev debug APK first; it launched but could not reach
    the dev server.
  - Built staging debug with `./gradlew :app:assembleStgDebug`.
  - Installed `apps/goatos-android/app/build/outputs/apk/stg/debug/app-stg-debug.apk`
    on `F5625U031150`.
  - Captured first-Poco staging launch artifacts:
    `director-stg-debug-launch.xml` and `director-stg-debug-launch-*.png`.
  - The first Poco now launches `sg.mesha.goatos.stg` and shows version
    `0.1.33-stg (code 34)` at the staging login screen.
  - The second Poco still launches `sg.mesha.goatos.stg` version `0.1.31-stg`
    at the staging login screen.
- Gcloud state changed:
  - `gcloud auth login --update-adc` succeeded through the browser.
  - `gcloud run services list --project goatos-stg --platform managed` now works.
  - Staging services in `asia-south1` include `goatos-api-stg`,
    `goatos-admin-web-stg`, and `goatos-kernel-worker-stg`.
  - `gcloud run jobs list --project goatos-stg` shows `goatos-stg-migrate`,
    `goatos-stg-analytics-rollup`, and `goatos-stg-outbox-dlq`.
  - `gcloud run services describe goatos-kernel-worker-stg` proves the staging
    kernel worker has `GOATOS_WORKER_STAGES_ENABLED=true` and tenant
    `00000000-0000-4000-8000-000000000001`.
  - Both `goatos-kernel-worker-stg` and `goatos-api-stg` are deployed at image
    `asia-south1-docker.pkg.dev/goatos-stg/goatos/backend:a5a64b7bbff4`, which
    is the branch base, not the local feature changes.
- Current remaining blockers:
  - both phones are at unauthenticated staging login, so real director/operator
    role E2E still needs credentials or a pre-authenticated session,
  - staging backend does not yet contain this branch, so live staging cannot
    prove the new inventory-vaccine behavior until this branch is deployed or an
    equivalent live backend is available.

## 2026-08-26 SyncEngine Proof Registration Coverage

- Added Android core-data regression coverage for the offline-to-online
  inventory proof registration path:
  - a completed proof-upload outbox row with server proof id
    `server-proof-9`,
  - a queued `PC_CARE_TASK_PROOF_REGISTER` row for task `task-1` and slot
    `stock_fridge_video`,
  - expected stable idempotency key
    `pc-care:task-proof:task-1:stock_fridge_video:proof-outbox-7`,
  - `SyncEngine.drainOnce()` calls `registerPcCareTaskProof(...)` with the
    resolved server proof ref and marks the registration row `SUCCEEDED`.
- Updated `ScriptedAppApi` to record `registerPcCareTaskProof` calls so the
  test proves the exact task id, proof slot, idempotency key, and proof ref sent
  to the API boundary.
- Verification:
  - from `apps/goatos-android/`,
    `./gradlew --console=plain -q :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.sync.SyncEngineTest.PC Care task proof registration resolves uploaded proof and uses stable idempotency key'`
    passed.
  - from `apps/goatos-android/`,
    `./gradlew --console=plain -q :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.

## 2026-08-26 Branch-Backed OCI DB And Two-Poco Evidence

- Use the OCI tunnel DB path for this feature branch. The latest OCI pass seeded
  a direct-SQL future vaccination drive assignment through the tunnel:
  - `planned_date = 2026-09-02`,
  - `physical_shed = Mandela 1`,
  - `partition_label = oci-proof-partition`,
  - `animal_count = 35`,
  - `vaccine_rule_ids = ET+TT + PPR`.
- Ran the real branch `cmd/kernel-worker` against the OCI tunnel DB with stages
  enabled:
  - operational startup logged
    `pc_care_inventory_vaccine_stage_complete`,
  - second operational tick logged
    `tasks_created=0`, `assignees_inserted=1`, `requirements_upserted=2`,
    proving idempotent replay and requirement fill.
- DB readback after the kernel run:
  - task id `d70cd09e-37c8-446a-85b5-b3251c16731a`,
  - category `inventory_vaccine`,
  - work state `scheduled`, status `open`,
  - due date `2026-08-26` for future vaccine date `2026-09-02`,
  - assigned to `Chandrakant` with `primary_role_hint = pc_director`,
  - requirements:
    - `ET+TT`: `10` doses,
    - `PPR`: `3` doses.
- Branch-backed API readback with a Chandrakant dev bearer token:
  - `GET /app/pc-care/tasks?category=inventory_vaccine&date=2026-08-26`
    returned HTTP 200 with the inventory task.
  - `GET /app/pc-care/worklist?category=inventory_vaccine&date=2026-08-26`
    returned HTTP 200 with the same task.
  - Response included `capture_mode = task_proof`, expected slot
    `stock_fridge_video`, assignee `Chandrakant`, and ET+TT/PPR requirement
    lines.
- First Poco, `F5625U031150`:
  - installed branch dev build `sg.mesha.goatos.dev` version `0.1.33-dev`,
  - minted and validated a Chandrakant/pc_director bearer token against the
    branch API (`/app/bootstrap` HTTP 200),
  - set `adb reverse tcp:8080 tcp:8081`,
  - granted camera, nearby/Bluetooth, notifications, location, and microphone,
  - foreground package confirmed as `sg.mesha.goatos.dev`.
  - Captured artifacts:
    - `director-dev-relaunched.xml`,
    - `director-dev-relaunched.png`,
    - permission/debug captures in the same evidence folder.
- Second Poco, `dd861eff`:
  - installed branch dev build `sg.mesha.goatos.dev`,
  - minted and validated an Amit/operator bearer token against the branch API
    (`/app/bootstrap` HTTP 200),
  - set `adb reverse tcp:8080 tcp:8081`,
  - foreground package confirmed as `sg.mesha.goatos.dev`,
  - captured `operator-dev-home.xml` / `operator-dev-home.png`.
  - The operator screenshot shows the branch-backed current drive surface with:
    - `Vaccines to carry`,
    - `13 doses`,
    - `ET+TT 10 doses`,
    - `PPR 3 doses`,
    - `Castro 1`,
    - current open counts.
- Remaining phone/UI gap after this pass:
  - the branch API returns the director inventory task correctly, but the
    Chandrakant dev app still landed on the regular vaccination calendar after
    launch; I did not capture a director top-card inventory-vaccine screen on
    the phone in this pass.
  - The prior Android unit/screenshot tests still prove the route/rendering
    components, but live phone surfacing for director-top-card remains a
    follow-up verification/fix item.

## 2026-08-26 Resume Checkpoint: Director PC Care Flag

- Added a focused assertion to `backend/internal/workforce/app/service_test.go` so `TestBootstrapPermissionDerivedExecutionFlags` now checks `pc_care_execute`.
- The test pins `pc_director`/Chandrakant behavior: with the PC Care leadership module available, bootstrap must return `pc_care_execute = true` so Android renders the executor/work-card face instead of the read-only monitor face.
- Verified with `cd backend && go test -count=1 ./internal/workforce/app -run TestBootstrapPermissionDerivedExecutionFlags`; passed.
- Re-read captured director XML artifacts. Latest director dev captures are still on the Vaccination Calendar surface, not the PC Care inventory-vaccine worklist. That keeps the physical-phone director top-card proof open even though backend permissions/API and Android unit/screenshot route coverage are green.

## 2026-08-26 Resume Checkpoint: Director Phone Top Card Fixed

- Root cause of the director phone gap: Chandrakant bootstrap already had `pc_care_execute = true` and the API returned the `inventory_vaccine` worklist, but Android cold-start chose the first drawer module (`vaccination` -> `/calendar`) instead of the backend-served active bar (`/pc/inventory-vaccine`).
- Backend fix: `pc_director` leadership module keys now put `pc_care` before the other preventive-care modules, and tests pin `pc_care_execute` plus Vaccine Stock first for pc_director.
- Android fix: `startDestinationFor` now honors the backend active `visible_navigation` root before falling back to the first drawer module href. Added `AppStartDestinationTest.pc director starts on active inventory bar even when drawer lists vaccination first`.
- Rebuilt/restarted the branch API backed by the OCI tunnel DB; direct Chandrakant bootstrap now returns `visible_navigation[0] = /pc/inventory-vaccine` and `pc_care_execute = true`.
- Rebuilt and reinstalled `sg.mesha.goatos.dev` on Poco `F5625U031150`, cleared app data, restored permissions/reverse tunnel, and launched against the branch API.
- New physical-phone evidence captured:
  - `director-dev-inventory-top-after-start-fix.png`
  - `director-dev-inventory-top-after-start-fix.xml`
- Screenshot visibly shows top landing as Preventive Care / Vaccine Stock with current card `Castro 1 - Parts 7-9`, status `Open`, due `2026-08-26`, assignee `Chandrakant`.
- Attempted to tap the card for detail proof capture evidence, but the tap did not drill in before this checkpoint; deeper proof registration remains covered by API/unit tests and still needs a live video submission pass.

Verification after this fix:

- `cd backend && go test -count=1 ./internal/workforce/app ./internal/permissions` passed.
- `cd apps/goatos-android && ./gradlew --console=plain -q :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.AppStartDestinationTest' --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest'` passed.
- `git diff --check` passed.

## 2026-08-26 Resume Checkpoint: Director Proof and Verifier Handoff

- Opened the director task on Poco `F5625U031150` using a long-press style tap
  after the top-card fix.
- Captured live branch-device evidence:
  - `director-dev-inventory-task-detail-open.png` / `.xml`,
  - `director-dev-inventory-after-proof-capture.png` / `.xml`,
  - `director-dev-inventory-after-submit-tap.png` / `.xml`,
  - `director-dev-inventory-after-submit-send.png` / `.xml`.
- The director proof screen showed `Fridge stock proof`, instruction text to
  show fridge vaccine stock, requirement lines `ET+TT 10 doses` and `PPR 3
  doses`, and then `Proof sent` plus `Submit task`.
- DB proof writeback after phone capture:
  - `pc_care_task_proofs.task_id = d70cd09e-37c8-446a-85b5-b3251c16731a`,
  - `slot_key = stock_fridge_video`,
  - `proof_ref = 27734c90-e68a-495b-8780-51e818baa527`.
- After pressing `Send`, the task moved to:
  - `status = pending_verification`,
  - `work_state = scheduled`,
  - `row_version = 2`,
  - `submitted_at = 2026-08-26 10:10:57 +05:30`.
- Found and fixed a real relay-envelope bug in the PC Care events:
  - `pc_care.task.pending_verification` was using `schema_version = v1`, which
    violates the shared event-envelope schema pattern.
  - PC Care used `aggregate_type = pc_care_task`, but
    `contracts/jsonschema/domain-event-envelope.schema.json` has no such enum
    value.
  - Fixed pending and completed PC Care envelopes to use schema-compatible
    values and added `backend/internal/pccare/adapters/postgres/outbox_envelope_test.go`
    so both PC Care event envelopes are validated by the same production
    validator as `cmd/outbox-relay`.
- Repaired the single already-created branch outbox payload to the new envelope
  shape, reset it from failed to pending, and ran:
  - `cd backend && go run ./cmd/outbox-relay -timeout=30s -limit=50`
- Relay result:
  - `claimed=2`,
  - `published=2`,
  - `failed=0`,
  - no `invalid_event_envelope`.
- Verifier handoff proof in DB:
  - verification item `bfee2192-e548-478a-9b48-f00cc36f1a24`,
  - `vertical = preventive_care`,
  - `module = pc_care`,
  - `category = inventory_vaccine`,
  - `source_ref_type = pc_care_task`,
  - `source_ref_id = d70cd09e-37c8-446a-85b5-b3251c16731a`,
  - `subject_label = Vaccine Inventory · Castro 1 - Parts 7-9`,
  - `status = pending`,
  - `operator_id = Chandrakant user id`,
  - `media_refs = ["27734c90-e68a-495b-8780-51e818baa527"]`.
- The relay also emitted a branch notification warning
  `verification_pending_notification_no_recipients`; that is verifier
  notification routing in the branch seed, not verifier item creation.

## 2026-08-26 Resume Checkpoint: Verifier Queue, Approval, Completion, And Stale-Card Filter

- Tried the real verifier queue as Jyothi
  (`90000000-0000-4000-8000-000000000104`) for
  `category=inventory_vaccine`.
- Found a real queue authorization gap:
  - `GET /verification/queue?category=inventory_vaccine&status=pending`
    returned HTTP 403 `module_scope_forbidden`.
  - Root cause: the PC Care bridge creates verifier items with module
    `pc_care`, but the branch verifier seed listed only
    `pc.vaccination`, `weighing`, `aas_health`, `counts`, and
    `feed.direction` verify duties.
- Fixed:
  - the branch seed helper now seeds Jyothi with
    `pc_care` verify duty.
  - `backend/internal/verification/module_key_translation.go` now explicitly
    preserves `pc_care` / `pc.care` in navigation-module translation.
  - Added
    `TestInventoryVaccineCategoryAllowsPCCareVerifierDuty` in
    `backend/internal/verification/adapters/http/r50_019_handler_test.go`.
- Added the missing `pc_care` verify duty to the already-running branch DB
  so the current E2E could continue without full reseed.
- Retried the real verifier queue as Jyothi:
  - HTTP 200,
  - item `bfee2192-e548-478a-9b48-f00cc36f1a24`,
  - `module = pc_care`,
  - `category = inventory_vaccine`,
  - subject `Vaccine Inventory · Castro 1 - Parts 7-9`,
  - operator `Chandrakant`,
  - media proof `27734c90-e68a-495b-8780-51e818baa527`,
  - evidence available.
- Submitted Jyothi approval through the real HTTP route:
  - `POST /verification/items/bfee2192-e548-478a-9b48-f00cc36f1a24/verdict`,
  - body `{"decision":"approved","row_version":1}`,
  - idempotency key `jyothi-inventory-vaccine-approve-1`,
  - HTTP 200,
  - verification item moved to `approved`,
  - `verified_by = 90000000-0000-4000-8000-000000000104`.
- The interval local relay processed the approval and applied it to PC Care:
  - `pc_care_tasks.status = completed`,
  - `pc_care_tasks.work_state = completed`,
  - `verified_by = Jyothi`,
  - `row_version = 3`.
- One stale outbox row was produced by the already-running old LaunchAgent
  binary before it was stopped; the source fix was already present, so the
  local row was repaired to the fixed envelope shape and replayed with
  current-source `cmd/outbox-relay`.
- Final local outbox chain is all published with valid envelopes:
  - `pc_care.task.pending_verification`,
  - `verification.item.pending`,
  - `verification.verdict.approved`,
  - `pc_care.task.completed`.
- Verifier pending queue after approval:
  - `GET /verification/queue?category=inventory_vaccine&status=pending`
    returned HTTP 200 with `items: []` and counts `approved: 1`.
- Chandrakant current-day worklist after approval:
  - `GET /app/pc-care/worklist?category=inventory_vaccine&date=2026-08-26`
    returned the task as `completed`, which is acceptable for same-day done
    cards.
- Chandrakant next-day worklist:
  - `GET /app/pc-care/worklist?category=inventory_vaccine&date=2026-08-27`
    returned `items: []`, proving the completed card does not carry forward as
    stale old work.
- Verification after this fix:
  - `cd backend && go test -count=1 ./internal/verification/... ./internal/pccare/... ./internal/kernelstages ./internal/workforce/app ./internal/permissions ./cmd/kernel-worker`
    passed.

## 2026-08-26 Resume Checkpoint: Admin-Web Progress Filter Tightened

- Audited the admin-web Vaccine Fridge Stock Checks progress section.
- Found one stale-card risk: the section filtered by work state and included
  every `completed` row the API returned. The backend date query should normally
  narrow this, but the product rule is stricter: same-day done can show, old
  completed work must not.
- Fixed `apps/admin-web/features/preventive-care-vaccination/inventory-vaccine-progress.tsx`:
  - scheduled and delayed inventory tasks remain visible,
  - completed inventory tasks are visible only when
    `task.due_business_date === asOf`.
- Updated
  `apps/admin-web/features/preventive-care-vaccination/inventory-vaccine-progress.test.mjs`
  to pin that same-day-only completed rule.
- Re-ran admin-web verification:
  - `cd apps/admin-web && npm test -- --test-name-pattern='inventory|pc care inventory|vaccination page mounts|verification action labels|filters out old'`
    passed (`481` tests discovered/passed under the project runner output).
  - `cd apps/admin-web && npm run typecheck` passed.

## 2026-08-26 Resume Checkpoint: Director Phone Shows Done After Verifier Approval

- Relaunched the branch dev app on Poco `F5625U031150` after Jyothi's verifier
  approval completed the PC Care task.
- Kept the phone pointed at the OCI-backed branch API with
  `adb reverse tcp:8080 tcp:8081`.
- Captured new physical-device evidence:
  - `director-dev-inventory-after-verifier-approval.png`,
  - `director-dev-inventory-after-verifier-approval.xml`.
- XML confirms the screen is the director's PC Care inventory route:
  - header `Preventive Care`,
  - title `Vaccine Stock`,
  - current day `Today · Wed 26 Aug`.
- XML confirms the same-day completed card behavior:
  - `Castro 1 - Parts 7-9`,
  - status chip `Done`,
  - `CPT · Due 2026-08-26`,
  - assignee `Chandrakant`.
- This is the physical-device counterpart to the earlier API evidence:
  - same-day completed card may remain visible as done,
  - next-day worklist returns empty and does not carry the finished card forward.

## 2026-08-26 Resume Checkpoint: Focused Validation Re-run

- Confirmed no stale validation processes were still running; only the Gradle
  daemon remained.
- Confirmed the branch is still `vaccine-inventory-director-task` at base
  commit `a5a64b7bbff4a0fc3d6f04c7760f5be4ef65ab2b`, with merge-base equal
  to `origin/main`.
- Re-ran focused admin-web validation with bounded logs:
  - `cd apps/admin-web && npm test -- --test-name-pattern='inventory|pc care inventory|vaccination page mounts|verification action labels|filters out old'`
    passed with `481` tests passing.
  - `cd apps/admin-web && npm run typecheck` passed.
- Re-ran `make api-client-check`.
  - It regenerated the API client successfully.
  - It exited non-zero at the final `git diff --exit-code -- packages/api-client/src/generated`
    step because this branch intentionally changes the app OpenAPI/generated
    client contract for `inventory_vaccine`, `task_proof`,
    `stock_fridge_video`, `PCCareInventoryRequirement`, and
    `/app/pc-care/tasks/{task_id}/proofs/{slot}`.
  - The generated client file is present in the worktree and matches the
    branch contract changes; the failure is the expected drift signal until the
    generated output is committed with the contract.
- Re-ran `git diff --check`; it passed with no whitespace errors.

## 2026-08-26 Resume Checkpoint: Fresh STG and OCI Tunnel Probe

- Re-checked OCI tunnel availability:
  - the canonical OCI DB env points at the tunnel endpoint;
  - the tunnel was not reachable during this pass.
- Re-confirmed browser-authenticated `gcloud` state:
  - active project: `goatos-stg`,
  - active account: `<active-gcloud-account>`,
  - Cloud Run services list includes `goatos-api-stg`,
    `goatos-admin-web-stg`, and `goatos-kernel-worker-stg`.
- Re-inspected deployed STG revisions/images:
  - `goatos-api-stg` latest ready revision
    `goatos-api-stg-00257-2l5`, image
    `asia-south1-docker.pkg.dev/goatos-stg/goatos/backend:a5a64b7bbff4`.
  - `goatos-admin-web-stg` latest ready revision
    `goatos-admin-web-stg-00230-trw`, image
    `asia-south1-docker.pkg.dev/goatos-stg/goatos/admin-web:a5a64b7bbff4`.
  - `goatos-kernel-worker-stg` latest ready revision
    `goatos-kernel-worker-stg-00227-7s6`, image
    `asia-south1-docker.pkg.dev/goatos-stg/goatos/backend:a5a64b7bbff4`.
  - STG worker still has `GOATOS_WORKER_STAGES_ENABLED=true`,
    `GOATOS_OUTBOX_PUBLISHER=pubsub`, topic
    `goatos-stg-outbox-events`, subscription
    `goatos-stg-domain-events`, and the domain-event schema path configured.
- This proves the production-style watcher/sweeper layer exists in STG, but
  also proves STG is not running this branch's new
  `pc_care_inventory_vaccine` stage yet.
- Re-ran the PC Care inventory integration discovery with explicit Postgres
  opt-in:
  - `cd backend && GOATOS_RUN_POSTGRES_TESTS=1 go test -count=1 ./internal/pccare/adapters/postgres -run 'Test(ReconcileInventoryVaccine|InventoryVaccine)' -v`
    exited `0`.
  - The six relevant tests were discovered; the later direct OCI fixture proof
    covers the tunnel DB/kernel readback path:
    `TestReconcileInventoryVaccineTasksCreatesDirectorTaskFromDriveAssignments`,
    `TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation`,
    `TestInventoryVaccineTaskUsesDriveAssignmentPartitionGrain`,
    `TestInventoryVaccineRequirementsUseExactDriveAssignmentMembers`,
    `TestInventoryVaccineChildTablesRejectTenantTaskMismatch`, and
    `TestInventoryVaccineTaskProofGatesSubmitAndFansOutToVerification`.
- The OCI readback for the unique fixture proved the director task assigned to
  Chandrakant with `ET+TT = 10` and `PPR = 25`; mutable fixture rows were
  cleaned afterward.

## 2026-08-26 Resume Checkpoint: Requirement Audit and Extra Local Checks

- Re-scanned current source for the requirement anchors:
  - OpenAPI exposes `inventory_vaccine`, `task_proof`, `stock_fridge_video`,
    `PCCareInventoryRequirement`, and
    `/app/pc-care/tasks/{task_id}/proofs/{slot}`.
  - Backend source contains the reconciler, kernel stage, task-proof storage,
    service/API category support, verifier fanout, and current/carry filtering.
  - Android source contains `/pc/inventory-vaccine`, proof-registration sync,
    route identity tests, inventory proof tests, screenshot tests, and
    current/carry date-window tests.
  - Admin-web source contains the `/vaccination` inventory progress section and
    verifier-review media coverage for PC Care inventory proofs.
- Re-ran targeted local checks tied directly to outstanding requirements:
  - `cd backend && go test -count=1 ./cmd/kernel-worker -run TestKernelWorkerSchedulesPcCareInventoryVaccineOnOperationalCadence -v`
    passed, proving the stage is registered on the operational 5-minute
    cadence.
  - `cd apps/admin-web && node --test features/preventive-care-vaccination/inventory-vaccine-progress.test.mjs features/verification-review/pc-care-inventory-proof.test.mjs`
    passed `6/6`, proving the CEO progress section, stale-card filter,
    vaccine dose rendering, and verifier media path contracts.
  - `cd apps/goatos-android && ./gradlew --console=plain -q :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest'`
    passed, proving Android refuses old execution dates, allows same-day done
    visibility, and keeps delayed carry-over visible.
- These checks do not close the OCI tunnel DB integration or live STG branch
  deployment gaps, but they strengthen the authoritative evidence for watcher
  registration, CEO/verifier UI, and no-old-card behavior.

## 2026-08-26 Resume Checkpoint: Procured/New-Animal Supporting Coverage

- Re-inspected the authored DB integration coverage for the remaining
  procured/new-animal path:
  - `TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation` creates a
    procured adult goat directly in the DB, publishes a vaccination rule, runs
    the real vaccination generation service, batches the generated obligation,
    creates a drive assignment, and then verifies the inventory-vaccine
    reconciler creates a `PPR = 1` director stock task.
  - The same integration file also covers partition-grain, exact assignment
    members, stale source cancellation, and child-table tenant/task FK parity.
- Because that DB test is still OCI-tunnel-gated, ran the local vaccination
  scheduling unit suite that proves the generated-obligation side of the path:
  - `cd backend && go test -count=1 ./internal/vaccination/app -run 'Procured|procured|SchedulePath|GenerateForVersion' -v`
    passed.
  - The passing tests include procured primary behavior, procurement warmup,
    adult blank-history campaigns, procurement purpose plans, adult-prior flags,
    kid/adult schedule path routing, and generated successor behavior.
- This strengthens the upstream evidence that new/procured animals get vaccine
  obligations based on age/procurement rules, but the full DB bridge from those
  generated obligations into inventory-vaccine director tasks remains covered by
  OCI-tunnel-gated integration tests rather than executable on this machine.

## 2026-08-26 Resume Checkpoint: Backend Contract and Scope Audit

- Reviewed the current diff surface with `git diff --stat`; changes remain
  concentrated in the expected areas:
  - PC Care backend/domain/API/reconciler/proof/verifier paths,
  - kernel-worker stage registration,
  - OpenAPI/generated app client,
  - Android PC Care route/proof/worklist sync and tests,
  - admin-web vaccination progress/verifier review,
  - phone-QA seed verifier duty.
- Searched the touched feature surface for leftover `TODO`, `FIXME`,
  `println`, `console.log`, and `panic`.
  - No feature debug leftovers were found.
  - The only hits were existing startup/error-path code in
    `backend/cmd/kernel-worker/main.go` and `backend/internal/kernelstages/bus.go`.
- Re-ran backend local contract tests for the API, service, verifier
  fanout, verifier duty, and permission route:
  - `cd backend && go test -count=1 ./internal/pccare/app ./internal/pccare/adapters/http ./internal/pccare/adapters/verificationbridge ./internal/verification/adapters/http ./internal/permissions -run 'InventoryVaccine|TaskProof|PCCare|Proof|Route|VerifierDuty' -v`
    passed.
  - Covered tests include:
    `TestCEOCanMonitorInventoryVaccineTasksTenantWide`,
    `TestRegisterTaskProofRequiresAssigneeAndValidatesLiveCameraMedia`,
    `TestPendingVerificationHandlerCarriesInventoryVaccineFridgeProof`,
    `TestOpenAPIIncludesInventoryVaccineTaskProofContract`,
    `TestTaskDTOIncludesInventoryVaccineWireShape`,
    `TestPutTaskProofRoutesInventoryFridgeProofToApp`,
    `TestRegisterCategoriesIncludesInventoryVaccine`,
    `TestEnqueueInventoryVaccineCreatesVerifierItemWithFridgeProof`,
    `TestInventoryVaccineCategoryAllowsPCCareVerifierDuty`, and
    `TestPCCareTaskProofRouteAllowsAssignedExecutors`.

## 2026-08-26 Resume Checkpoint: Contract Validation and Client Generation

- Ran `npm --prefix tools/contract-validation run validate`; the first attempt
  failed before validating contracts because the validator's local
  `node_modules` were missing (`ERR_MODULE_NOT_FOUND:
  @apidevtools/swagger-parser`).
- Installed the validator's locked dependencies with
  `npm --prefix tools/contract-validation ci --no-audit --no-fund`; this added
  no tracked files.
- Re-ran `npm --prefix tools/contract-validation run validate`; it passed:
  - `Validated 4 OpenAPI specs, 7 JSON Schemas, and 5 example payloads.`
- Re-ran `npm --prefix packages/api-client run generate`; it passed and
  regenerated app/admin/analytics clients.
- Checked the generated-client diff; only the expected tracked files remain
  changed:
  - `contracts/openapi/app-api.yaml`,
  - `packages/api-client/src/generated/app-api.ts`.
- The generated diff is limited to the intended app API additions for
  `inventory_vaccine`, `task_proof`, `stock_fridge_video`,
  `PCCareInventoryRequirement`, the task-proof PUT route, and updated PC Care
  task submit description.

## 2026-08-26 Resume Checkpoint: Fresh Cross-Surface Local Bundle

- Re-ran the compact local validation bundle across the implemented surfaces:
  - `cd backend && go test -count=1 ./internal/verification/... ./internal/pccare/... ./internal/kernelstages ./internal/workforce/app ./internal/permissions ./cmd/kernel-worker`
    passed.
  - `cd apps/admin-web && npm test -- --test-name-pattern='inventory|pc care inventory|vaccination page mounts|verification action labels|filters out old'`
    passed with `481` tests.
  - `cd apps/admin-web && npm run typecheck` passed.
  - `make migration-duplicate-versions-guard` passed.
  - `npm --prefix tools/contract-validation run validate` passed.
  - `cd apps/goatos-android && ./gradlew --console=plain -q :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.sync.SyncEngineTest.PC Care task proof registration resolves uploaded proof and uses stable idempotency key'`
    passed.
  - `cd apps/goatos-android && ./gradlew --console=plain -q :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.AppStartDestinationTest' --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'`
    passed.
- This gives a single fresh backend/admin-web/contract/migration/Android
  snapshot for the branch-local implementation. It still does not replace the
  unavailable OCI tunnel DB integration replay or live STG proof against
  this branch.

## 2026-08-26 Resume Checkpoint: Container Runtime and Migration Guard Probe

- Checked canonical OCI tunnel availability for DB replay:
  - helper: `tools/local/oci-goatos-a1-dev.sh`;
  - env: `local-data/goatos-stg-to-oci/oci-goatos-db.env`;
  - tunnel command: `tools/local/oci-goatos-a1-dev.sh tunnel`;
  - current check: tunnel was not reachable.
- This means OCI DB replay still needs the OCI tunnel opened before it can run from this machine.
- Re-ran the local hot-index migration guard:
  - `make validate-hot-index-migrations` still exits non-zero on existing
    historical migration debt.
  - The violation list does not include
    `000211_pc_care_inventory_vaccine.sql`.
  - A direct search of the guard output plus the new migration only finds the
    expected new table/index definitions in `000211`, not a reported violation.

## 2026-08-26 Resume Checkpoint: Repeatable Local Verification Helper

- Added `tools/dev/vaccine-inventory-director-verify.sh` to avoid future
  resume loops over the same branch-local checks.
- The helper writes a report under
  `.codex-goatos-render/vaccine-inventory-director/<run-id>/report.md` and
  logs every command separately.
- It runs:
  - backend focused verification/PC Care/kernel/workforce/permission tests,
  - vaccination procurement scheduling support tests,
  - admin-web inventory progress tests plus typecheck,
  - contract validation,
  - migration duplicate-version guard,
  - Android PC Care task-proof sync test,
  - Android inventory route/worklist/proof UI tests.
- It checks OCI tunnel DB availability:
  - when the OCI tunnel is reachable, it records the OCI DB check as passed;
  - when the OCI tunnel is not open, it records the DB check as `skipped` with
    the tunnel command to run.
- Ran the helper with
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826`:
  - exit `0`,
  - all local steps passed.
- Generated report:
  - `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826/report.md`.

## 2026-08-26 Resume Checkpoint: Helper Includes STG Image Inspection

- Extended `tools/dev/vaccine-inventory-director-verify.sh` so each run also
  records STG Cloud Run revision/image information when `gcloud` is available.
- The STG check is report-only and local:
  - it inspects `goatos-api-stg`,
  - `goatos-admin-web-stg`,
  - and `goatos-kernel-worker-stg`.
- Re-ran the helper with
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-stg`:
  - exit `0`,
  - all local checks passed,
  - STG inspection succeeded.
- Generated report:
  - `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-stg/report.md`.
- The report shows STG still on base images:
  - `goatos-api-stg-00257-2l5` -> `backend:a5a64b7bbff4`,
  - `goatos-admin-web-stg-00230-trw` -> `admin-web:a5a64b7bbff4`,
  - `goatos-kernel-worker-stg-00227-7s6` -> `backend:a5a64b7bbff4`.

## 2026-08-26 Resume Checkpoint: OCI DB Direct Kernel Evidence

- Reopened the OCI DB tunnel through the workspace helper and verified the
  branch migration objects exist on the tunnel target:
  - `pc_care_task_inventory_requirements`,
  - `pc_care_task_proofs`.
- Seeded a unique OCI fixture for tenant
  `00000000-0000-4000-8000-000000000001`:
  - vaccination date `2026-09-02`,
  - task date `2026-08-26`,
  - park `Channapatna`,
  - shed `Mandela 1 - Part 7`,
  - partition `oci-proof-partition`,
  - 10 ET+TT obligations,
  - 25 PPR obligations.
- Ran the real branch `cmd/kernel-worker` against the OCI tunnel DB with stages
  enabled and nondurable local eventbus publisher. The worker startup ran the
  `pc-care-inventory-vaccine` stage on the operational lane and logged stage
  completion. Other unrelated lanes hit calendar/time-limit failures before the
  short proof timeout, so the process exit was nonzero, but the target stage ran.
- Replayed the exact reconciler CTE shape against OCI for deterministic readback.
  It inserted/reconciled the director assignment and requirement rows for the
  live T-7 source tasks.
- OCI readback for the unique fixture proved:
  - task category `inventory_vaccine`,
  - planned/due business date `2026-08-26`,
  - status/work state `open/scheduled`,
  - assignee `Chandrakant`,
  - `ET+TT = 10`,
  - `PPR = 25`,
  - source batch `9d260826-0000-4000-8000-000000008001`.
- Cleaned the mutable OCI fixture rows afterward:
  - 2 requirement rows,
  - 1 assignee row,
  - 1 task row,
  - 1 drive assignment,
  - 35 obligations,
  - 1 obligation batch.
- Cleanup verification returned zero remaining task, assignment, obligation, and
  batch rows for the fixture IDs. One inert published protocol version remains
  because the database correctly prevents deleting published protocol config
  through its immutability trigger.
- Final helper pass for this checkpoint:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-final-pass tools/dev/vaccine-inventory-director-verify.sh`
  exited `0`; backend, procurement scheduling, admin-web, contract validation,
  migration duplicate guard, Android proof sync, Android route/worklist tests,
  and `oci_db_check` all passed.
- The default verification helper intentionally keeps the OCI check
  non-mutating. The direct DB/kernel fixture proof above is a controlled OCI pass
  with explicit seed, readback, cleanup, and cleanup verification.
- Re-ran the non-mutating helper with
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-nonmutating-final`:
  exit `0`, `oci_db_check=reachable`, STG inspection succeeded, and
  `stg_proves_current_worktree=false` because STG is still on the base commit
  image while this feature remains uncommitted worktree state.

## 2026-08-26 Resume Checkpoint: One-Pass Reconciler Fix

- The controlled OCI proof exposed a first-pass issue: the worker could create a
  new `inventory_vaccine` task shell on the first reconciliation pass, then fill
  assignees/requirements on replay. Root cause was the CTE shape: sibling CTEs
  reading `pc_care_tasks` did not see the data-modifying `inserted_tasks` rows.
- Fixed `ReconcileInventoryVaccineTasks` so `live_tasks` includes
  `inserted_tasks UNION existing tasks`. Newly inserted tasks now receive
  assignees and inventory requirements in the same reconciliation pass.
- Focused backend verification passed:
  `cd backend && go test -count=1 ./internal/pccare/... ./internal/kernelstages ./cmd/kernel-worker`.
- Re-ran a controlled OCI one-pass fixture:
  - vaccination date `2026-09-02`,
  - task date `2026-08-26`,
  - partition `oci-proof-onepass`,
  - 10 ET+TT obligations,
  - 25 PPR obligations.
- A single call to the fixed reconciler returned:
  `tasks_created=1 assignees_inserted=1 requirements_upserted=8 directors=1`.
  The requirement count includes other live T-7 tasks in the shared OCI DB, so
  the unique fixture was read back directly.
- OCI readback for `oci-proof-onepass` proved:
  - category `inventory_vaccine`,
  - due `2026-08-26`,
  - status/work state `open/scheduled`,
  - assignee `Chandrakant`,
  - `ET+TT = 10`,
  - `PPR = 25`.
- Cleaned the mutable one-pass fixture rows afterward:
  - 2 requirement rows,
  - 1 assignee row,
  - 1 task row,
  - 1 drive assignment,
  - 35 obligations,
  - 1 obligation batch.
- Cleanup verification returned zero remaining task, assignment, obligation, and
  batch rows for the one-pass fixture IDs.
- Re-ran the non-mutating helper after the fix:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-onepass-fix tools/dev/vaccine-inventory-director-verify.sh`
  exited `0`; all focused backend, procurement scheduling, admin-web, contract,
  migration guard, Android, and OCI DB reachability checks passed.
- Added cheap source-level regression guard
  `TestInventoryVaccineLiveTasksIncludesInsertedRows` so a future edit cannot
  drop `inserted_tasks` from `live_tasks` and reintroduce the first-pass shell
  task bug before DB/OCI tests catch it.
- Verification:
  - `cd backend && go test -count=1 ./internal/pccare/adapters/postgres -run 'TestInventoryVaccineLiveTasksIncludesInsertedRows|TestReconcileInventoryVaccine' -v`
    passed the source guard and discovered the opt-in DB test.
  - `cd backend && go test -count=1 ./internal/pccare/... ./internal/kernelstages ./cmd/kernel-worker`
    passed.

## 2026-08-26 Resume Checkpoint: Helper Hygiene Gate

- Added a `feature docs and scripts hygiene` step to
  `tools/dev/vaccine-inventory-director-verify.sh`.
- The gate scans the feature progress doc, final coverage report, helper script,
  and saved E2E artifact folder for:
  - personal home paths/names,
  - non-OCI DB guidance,
  - non-OCI container-runtime DB guidance.
- The patterns are constructed at runtime so the helper does not contain the
  forbidden strings literally.
- Re-ran the helper with
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-hygiene-gate`:
  exit `0`; all previous focused gates passed, the new hygiene gate passed,
  and `oci_db_check=reachable`.
- Tightened helper console output so failure log paths and final `report=...`
  output use repo-relative labels instead of absolute machine paths.
- Re-ran the helper with
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-relative-output`:
  exit `0`; all gates passed, `oci_db_check=reachable`, and the generated
  report/commands file contains only relative artifact paths.

## 2026-08-26 Resume Checkpoint: Helper Distinguishes Commit vs Worktree

- Tightened `tools/dev/vaccine-inventory-director-verify.sh` metadata:
  - records branch name,
  - full and short `HEAD`,
  - merge-base with `origin/main`,
  - whether the worktree is dirty,
  - whether STG images match the `HEAD` commit tag,
  - and whether STG proves the current worktree.
- Re-ran the helper with
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-worktree-meta`:
  - exit `0`,
  - all local checks passed,
  - OCI tunnel DB check is covered by the later `codex-oci-20260826-relpaths`
    helper run with `oci_db_check=reachable`,
  - STG inspection succeeded.
- Generated report:
  - `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-worktree-meta/report.md`.
- The report records:
  - branch `vaccine-inventory-director-task`,
  - `HEAD`/merge-base `a5a64b7bbff4a0fc3d6f04c7760f5be4ef65ab2b`,
  - `worktree_dirty = true`,
  - `stg_images_match_head_commit = true`,
  - `stg_proves_current_worktree = false`.
- This is the precise live-STG gap: STG is on the base commit image, while the
  feature implementation is still uncommitted local worktree state.

## 2026-08-26 Resume Checkpoint: Fresh API Client And OCI Helper Gate

- Re-ran `make api-client-check`.
  - `npm --prefix packages/api-client run generate` succeeded for app, admin,
    and analytics clients.
  - The target then exited non-zero at
    `git diff --exit-code -- packages/api-client/src/generated`, because this
    uncommitted branch intentionally changes the generated app client relative
    to `HEAD`.
  - The generated diff is limited to the expected feature additions:
    `inventory_vaccine`, `task_proof`, `stock_fridge_video`,
    `PCCareInventoryRequirement`, and
    `/app/pc-care/tasks/{task_id}/proofs/{slot}`.
- Opened the OCI tunnel with the workspace helper and re-ran the consolidated
  feature helper:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-oci-reachable-after-doc-clean tools/dev/vaccine-inventory-director-verify.sh`.
  Result: exit `0`; backend focused suite, vaccination procurement support,
  admin-web inventory progress/typecheck, contract validation, migration
  duplicate-version guard, Android proof sync, Android route/worklist tests,
  feature docs/scripts hygiene, and `oci_db_check=reachable` all passed.
- Generated verification report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-oci-reachable-after-doc-clean/report.md`.
- STG inspection in that report still shows `stg_proves_current_worktree=false`
  because Cloud Run is on base image tag `a5a64b7bbff4` while this feature is
  uncommitted branch worktree state.

## 2026-08-26 Resume Checkpoint: Independent Judge Fixes

- Ran two independent read-only judge agents:
  - backend/kernel/API/DB review,
  - Android/admin-web review.
- Fixed backend/API findings:
  - added `ErrKernelOwnedCategory` and rejected planner/API create for
    `inventory_vaccine`,
  - submit now refuses inventory tasks with fridge proof but no
    `pc_care_task_inventory_requirements` rows,
  - stale cleanup now cancels source-less open/delayed inventory tasks whose
    source drive disappeared even after long carry-over,
  - task DTOs and OpenAPI now include `task_proofs` for server-visible
    task-level proof refresh.
- Fixed Android/admin-web findings:
  - admin inventory progress fetches all pages and uses `current_or_carry`,
  - Android consumes server `task_proofs` so another director/device can see an
    already captured fridge proof and submit,
  - inventory proof Photo/Video controls use a wrapping layout and the
    screenshot test now runs on a compact viewport.
- Verification after these fixes:
  - backend focused package run passed,
  - admin inventory/verifier tests and typecheck passed,
  - Android inventory proof and screenshot focused tests passed,
  - consolidated helper run
    `codex-resume-20260826-judge-fixes-oci` passed all focused gates with
    `oci_db_check=reachable`.

## 2026-08-26 Resume Checkpoint: OCI And STG Refresh

- Opened the OCI tunnel with the workspace OCI helper.
- Ran the consolidated feature verifier:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-oci-stg-refresh tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`.
- Passed gates:
  - backend focused vaccine inventory suite,
  - vaccination procurement scheduling support,
  - admin-web inventory progress/typecheck,
  - contract validation,
  - migration duplicate-version guard,
  - Android task-proof sync,
  - Android inventory route/worklist tests,
  - feature docs/scripts hygiene,
  - OCI tunnel database check.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-oci-stg-refresh/report.md`.
- STG Cloud Run inspection succeeded for API, admin-web, and kernel-worker.
- Remaining live gap is unchanged:
  `stg_proves_current_worktree=false` because STG is on the base commit image
  while this feature is still uncommitted worktree state.

## 2026-08-26 Resume Checkpoint: Origin Main Refreshed

- Inspected the canonical STG deploy contract and Cloud Deploy runbooks before
  attempting any staging mutation.
- Deploy finding:
  - normal STG deploy authority is the Slack/Cloud Build/Cloud Deploy path from
    latest approved `origin/main`,
  - dirty worktree deploys and direct Cloud Run service updates are not valid
    normal STG deploy paths.
- Fast-forwarded this branch to latest `origin/main`.
- Re-ran the consolidated feature verifier:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-after-origin-main-refresh tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all gates passed and `oci_db_check=reachable`.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-after-origin-main-refresh/report.md`.
- Current metadata:
  - `HEAD == origin/main`,
  - `worktree_dirty=true`,
  - `stg_images_match_head_commit=false`,
  - `stg_proves_current_worktree=false`.
- Remaining live gap:
  STG Cloud Run still points at the older base image, so full browser/Poco
  branch-backed role-flow E2E remains unproven until the feature is committed,
  approved, and deployed through the authorized STG path.

## 2026-08-26 Resume Checkpoint: Changed Script Hygiene

- Widened the hygiene audit from the feature docs/helper to every changed or
  newly added text file in the branch.
- Found one branch-touched script with stale local-port guidance:
  `tools/local/phone-qa-throwaway-seed.sh`.
- Updated that script so the touched branch version now requires:
  - `DATABASE_URL` from the OCI DB environment,
  - an open OCI tunnel,
  - `GOATOS_PHONE_QA_SEED_CONFIRM=oci-phone-qa`,
  - `GOATOS_ENV=stg` or `GOATOS_ENV=oci`.
- Preserved the feature's `pc_care` grant addition for phone-QA verifier access.
- Re-ran:
  - `bash -n tools/local/phone-qa-throwaway-seed.sh`,
  - `bash -n tools/dev/vaccine-inventory-director-verify.sh`,
  - `git diff --check`.
- Result: all passed.
- Broad changed-file scan now has no branch-added docs/scripts non-OCI database
  or container-runtime guidance. Remaining loopback URL hits are pre-existing
  admin-web runtime fallback code, not feature docs/scripts.

## 2026-08-26 Resume Checkpoint: Helper Wide Hygiene Gate

- Moved the widened hygiene audit into
  `tools/dev/vaccine-inventory-director-verify.sh` so the branch self-checks
  changed/new docs, scripts, XML artifacts, and other text handoff files.
- The helper now builds a changed-file target list and scans it for:
  - personal path/name leakage,
  - non-OCI database guidance,
  - non-OCI container-runtime guidance,
  - loopback address guidance in docs/scripts/artifacts.
- First full helper run with this wider gate failed because the helper scanned
  its own pattern text. Fixed the helper to construct that pattern without
  writing it literally.
- Re-ran:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-helper-wide-hygiene-fixed tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all gates passed and `oci_db_check=reachable`.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-helper-wide-hygiene-fixed/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Current API Client Drift

- Re-ran `make api-client-check` after the `origin/main` refresh and helper
  hygiene-gate change.
- Result:
  - API client generation succeeded for app, admin, and analytics clients.
  - The target then exited at the expected generated-client diff gate because
    this branch intentionally changes the app OpenAPI contract and generated
    app client.
- Diff scope:
  - `contracts/openapi/app-api.yaml`,
  - `packages/api-client/src/generated/app-api.ts`.
- Expected terms confirmed in the diff:
  - `inventory_vaccine`,
  - `task_proof`,
  - `stock_fridge_video`,
  - `PCCareInventoryRequirement`,
  - `PCCareTaskProof`,
  - `task_proofs`,
  - `current_or_carry`,
  - `appRegisterPCCareTaskProof`.

## 2026-08-26 Resume Checkpoint: API Client Gate In Helper

- Added an API client generation drift-scope gate to
  `tools/dev/vaccine-inventory-director-verify.sh`.
- The helper now:
  - runs API client generation,
  - requires generated-client drift to stay scoped to
    `packages/api-client/src/generated/app-api.ts`,
  - verifies expected vaccine-inventory contract terms in the OpenAPI/generated
    app-client diff.
- Re-ran the full OCI-backed helper:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-api-client-gate tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all gates passed and `oci_db_check=reachable`.
- New passing gate in the report:
  `api client generation drift scope`.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-api-client-gate/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Current Worktree Inventory

- Current branch base:
  `c08d21f0589736d901d6e9cb69aeb71db80e521d`.
- `HEAD == origin/main` before the uncommitted feature worktree changes.
- Current inventory:
  - 51 modified tracked files,
  - 20 untracked status entries.
- Note: `git status --short` groups screenshot/XML evidence directories as
  single entries, so this is the current status-line inventory rather than a
  recursive file count.
- Main change groups:
  - backend PC Care inventory reconciler/proof/domain/API/verifier flow,
  - kernel worker inventory vaccine stage,
  - OpenAPI and generated app client,
  - Android route/proof/sync/worklist/UI tests,
  - admin-web progress and verifier media tests,
  - OCI-only phone-QA seed guard and `pc_care` grant,
  - consolidated verifier helper,
  - progress/final coverage docs and Poco screenshot/XML evidence.
- Generated `.codex-goatos-render/vaccine-inventory-director/` reports remain
  referenced evidence artifacts, not git-tracked deliverables.

## 2026-08-26 Resume Checkpoint: Helper Readability Refactor

- Refactored `tools/dev/vaccine-inventory-director-verify.sh` so the two dense
  gates are named shell functions:
  - `api_client_generation_drift_scope`,
  - `feature_docs_and_scripts_hygiene`.
- Preserved the same helper step labels and gate behavior.
- Re-ran the full OCI-backed helper:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-helper-refactor tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all gates passed and `oci_db_check=reachable`.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-helper-refactor/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Source Marker Audit

- Built a changed/new source-file list with 68 files.
- Scanned for common debug or temporary markers:
  `TODO`, `FIXME`, `console.log`, `println`, stack-trace calls, debugger
  statements, and dump helpers.
- Result:
  - no actionable debug/temporary markers found,
  - only expected startup error handling remains:
    `fmt.Fprintln(os.Stderr, err)` in `backend/cmd/kernel-worker/main.go`.
- Tightened the touched phone-QA seed script's human-facing wording:
  - output now says OCI phone QA DB,
  - CPT fixture description now says OCI fixture.

## 2026-08-26 Resume Checkpoint: Full Helper After Source Cleanup

- Re-ran the full consolidated verifier after the source-marker and seed-script
  wording cleanup:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-source-marker-cleanup tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`.
- Passed gates:
  - backend focused vaccine inventory suite,
  - vaccination procurement scheduling support,
  - admin-web inventory progress/typecheck,
  - contract validation,
  - API client generation drift scope,
  - migration duplicate-version guard,
  - Android task-proof sync,
  - Android inventory route/worklist tests,
  - feature docs/scripts hygiene,
  - OCI tunnel database check.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-source-marker-cleanup/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Source Marker Gate In Helper

- Added `source_marker_audit` to
  `tools/dev/vaccine-inventory-director-verify.sh`.
- The gate scans changed/new source files for common debug markers while
  allowing the expected kernel-worker startup stderr line.
- First run caught the helper's own marker-pattern text; fixed the helper to
  construct those marker patterns without writing them literally.
- Re-ran the full OCI-backed helper:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-source-marker-gate-fixed tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all gates passed and `oci_db_check=reachable`.
- New passing gate:
  `source marker audit`.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-source-marker-gate-fixed/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: STG Deploy Contract Gate

- Added `stg_deploy_contract_guard` to
  `tools/dev/vaccine-inventory-director-verify.sh`.
- The guard checks `context/deploy-contract.json` for the Slack/Cloud
  Build/Cloud Deploy authority, `origin/main` source ref, clean worktree
  requirement, and break-glass-only deploy scripts.
- The guard also checks the STG runbooks for the same normal-deploy boundary.
- Re-ran the full OCI-backed helper:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-deploy-contract-gate tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all gates passed and `oci_db_check=reachable`.
- New passing gate:
  `stg deploy contract guard`.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-deploy-contract-gate/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Remaining Closeout Checklist

- Goal cannot be marked complete until live STG is branch-backed.
- Remaining closeout proof:
  - commit/approve the feature through the normal repo path,
  - deploy through the authorized Slack/Cloud Build/Cloud Deploy path from
    approved `origin/main`,
  - rerun `tools/dev/vaccine-inventory-director-verify.sh` and confirm deployed
    feature evidence, ideally `stg_proves_current_worktree=true` for a clean
    committed feature,
  - capture live STG role-flow evidence for director/operator and CEO/verifier
    surfaces after deploy.
- Do not use direct Cloud Run service mutation or non-OCI database/container
  shortcuts as completion evidence.

## 2026-08-26 Resume Checkpoint: Requirement Evidence Wording Audit

- Re-audited the progress and final coverage documents against the current
  consolidated helper evidence.
- Updated the historical requirement table so branch-proved items say
  `Proved branch-side` instead of stale `Partially proved` wording.
- Kept the live STG boundary explicit:
  `stg_proves_current_worktree=false`.
- Tightened the final coverage report wording for overdue/carry-over,
  procurement/manual-animal, partition/shifting grain, and stale-source edits
  from authored coverage to passing branch coverage where the helper suite now
  proves those paths.
- Re-ran syntax, whitespace, and targeted hygiene checks; no forbidden
  personal path/name or non-OCI database/container guidance appeared in the
  touched docs/scripts.

## 2026-08-26 Resume Checkpoint: Expanded Source Hygiene Gate

- Removed stale postgres integration-test comment wording that named the old
  test harness directly.
- Expanded `feature_docs_and_scripts_hygiene` with a changed-source scan so
  stale non-OCI database/container guidance cannot hide in touched source
  comments.
- The first full helper run with the new scan caught the verifier's own
  allow-list regex; fixed that by constructing the regex dynamically.
- Re-ran the full OCI-backed helper:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-source-hygiene-self-hit-fixed tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all gates passed and `oci_db_check=reachable`.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-source-hygiene-self-hit-fixed/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Completion Snapshot Consistency Audit

- Re-read the final coverage report's requirement matrix, remaining gaps, and
  completion snapshot against the latest helper report.
- Found and fixed a wording contradiction where the completion snapshot blended
  the branch-backed phone fixture with the OCI direct-DB stock-count fixture.
- The final report now states:
  - phone proof E2E fixture: `ET+TT 10`, `PPR 3`,
  - requested OCI direct-DB stock-count fixture: `ET+TT 10`, `PPR 25`.
- Re-ran syntax, whitespace, targeted count scan, and hygiene checks.
- Result: all focused checks passed; `stg_proves_current_worktree=false`
  remains the only completion boundary.

## 2026-08-26 Resume Checkpoint: Completion Ledger Added

- Added a `Completion Ledger` near the top of the final coverage report.
- The ledger maps each explicit requirement to:
  - current evidence verdict,
  - remaining proof needed before the overall goal can be complete.
- Tightened the T-7 task row status from authored coverage to passing DB
  coverage.
- Re-ran syntax, whitespace, and targeted hygiene checks.
- Result: all focused checks passed; the only remaining proof is still live STG
  serving this committed feature and role-flow evidence after that deploy.

## 2026-08-26 Resume Checkpoint: Final Report Consistency Guard

- Added `final_report_consistency_guard` to
  `tools/dev/vaccine-inventory-director-verify.sh`.
- The guard asserts the final coverage report contains:
  - the `Completion Ledger`,
  - the latest full OCI-backed helper run id,
  - the explicit `stg_proves_current_worktree=false` boundary,
  - the key requirement rows for T-7 creation, watcher, overdue carry-over,
    stale-card filtering, director proof, CEO/verifier surfaces,
    procurement/manual animal generation, and shifting/partition coverage,
  - the requested OCI stock-count fixture shape `ET+TT 10` and `PPR 25`.
- Ran the helper with:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-final-report-guard tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all code/doc gates including the new final report
  consistency guard passed. OCI DB check was skipped in this focused run because
  the tunnel was not open; at that point, the latest full OCI-backed passing run remained
  `codex-resume-20260826-source-hygiene-self-hit-fixed`.

## 2026-08-26 Resume Checkpoint: Kernel-Owned Planner Guard

- Audited the planner/create path for the kernel-owned `inventory_vaccine`
  category.
- Existing create guard rejected manual `inventory_vaccine` task creation, but
  the planner catalog still built categories from the full read vocabulary.
- Added `domain.PlannerCategories` and `domain.IsKernelOwnedCategory`.
- Updated planner catalog responses to offer only human-plannable PC Care
  categories while keeping `inventory_vaccine` valid for monitor/worklist reads.
- Updated planner shed reads and create writes to reject kernel-owned categories
  with `ErrKernelOwnedCategory`.
- Added service and HTTP regression tests:
  - `TestPlannerParkShedsRejectsKernelOwnedInventoryVaccineCategory`,
  - `TestPlannerCatalogExcludesKernelOwnedInventoryVaccineCategory`.
- Updated OpenAPI/generated client wording to state that kernel-owned
  `inventory_vaccine` tasks are visible on monitor/worklist reads but not
  offered by the create wizard.
- Ran focused backend tests:
  `cd backend && go test -count=1 ./internal/pccare/app ./internal/pccare/adapters/http -run 'KernelOwned|PlannerCatalog|InventoryVaccine|TaskProof' -v`.
- Ran the consolidated helper with:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-kernel-owned-planner-guard tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all code/doc gates passed. OCI DB check was skipped in
  this focused run because the tunnel was not open; at that point, the latest
  full OCI-backed passing run remained
  `codex-resume-20260826-source-hygiene-self-hit-fixed`.

## 2026-08-26 Resume Checkpoint: Full OCI Refresh After Planner Guard

- Reopened the OCI tunnel and re-ran the full consolidated helper after the
  kernel-owned planner guard landed:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-kernel-owned-full-oci tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all gates passed, including:
  - backend focused vaccine inventory suite,
  - vaccination procurement scheduling support,
  - admin-web inventory progress/typecheck,
  - contract validation,
  - API client generation drift scope,
  - migration duplicate-version guard,
  - Android task-proof sync,
  - Android inventory route/worklist tests,
  - feature docs/scripts hygiene,
  - source marker audit,
  - STG deploy contract guard,
  - final report consistency guard,
  - OCI tunnel database check.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-kernel-owned-full-oci/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Worktree Inventory Recheck

- Recomputed the feature worktree inventory from git after the latest full
  OCI-backed run and planner guard changes.
- Current counts still match the final coverage report:
  - modified tracked files: `51`,
  - untracked status entries: `20`.
- Updated the final report wording so the untracked count is clearly the
  current status-line inventory rather than the older recursive evidence-file
  count.

## 2026-08-26 Resume Checkpoint: API Drift Planner Guard

- Tightened `api_client_generation_drift_scope` in
  `tools/dev/vaccine-inventory-director-verify.sh`.
- The generated-client drift gate now requires the planner guard wording from
  OpenAPI/generated app client diffs:
  - `human-plannable`,
  - `Kernel-owned`,
  - `create wizard`.
- This prevents future contract regeneration from silently losing the fact that
  `inventory_vaccine` remains readable on monitor/worklist surfaces but is not
  offered by the human planner create wizard.
- Ran the helper with:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-api-drift-planner-guard tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all code/doc gates passed. OCI DB check was skipped in
  this focused run because the tunnel was not open; at that point, the latest
  full OCI-backed passing run remained
  `codex-resume-20260826-kernel-owned-full-oci`.

## 2026-08-26 Resume Checkpoint: Full OCI Refresh After API Drift Guard

- Reopened the OCI tunnel and re-ran the full consolidated helper after the API
  drift planner guard landed:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-api-drift-full-oci tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; every gate passed, including the tightened API client
  generation drift scope and `oci_db_check=reachable`.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-api-drift-full-oci/report.md`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Generated Client Drift Recheck

- Rechecked generated-client drift after the latest full OCI-backed verifier.
- Current generated/API diff is scoped to:
  - `contracts/openapi/app-api.yaml`,
  - `packages/api-client/src/generated/app-api.ts`.
- Confirmed there is no generated drift in:
  - `packages/api-client/src/generated/admin-api.ts`,
  - `packages/api-client/src/generated/analytics-api.ts`,
  - `contracts/openapi/admin-api.yaml`,
  - `contracts/openapi/analytics-api.yaml`.
- Confirmed the verifier latest-run guard points to
  `codex-resume-20260826-api-drift-full-oci`.

## 2026-08-26 Resume Checkpoint: Broad Text Hygiene Recheck

- Rebuilt the changed/untracked text-file target list from git and scanned
  `70` text targets for personal identifiers, machine-specific paths, and
  non-OCI database/container guidance.
- Feature docs and scripts remain clean for the OCI-only requirement.
- The broad scan found only two loopback API fallback strings in
  `apps/admin-web/lib/api/server.ts`; both are present on `origin/main` and
  were not introduced by this feature. The feature diff in that file only adds
  the current/carry PC-care task list call.
- Re-ran the scan after removing a self-hit in this checkpoint text. The
  hygiene scan then had no hits; the marker audit found only the expected
  kernel-worker startup stderr line in `backend/cmd/kernel-worker/main.go` plus
  historical report lines documenting that same expected exception.

## 2026-08-26 Resume Checkpoint: Focused Doc-Hygiene Helper Run

- Ran the consolidated helper without the OCI tunnel open:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-doc-hygiene-focused tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; backend, vaccination procurement support, admin-web,
  contract validation, generated-client drift scope, migration guard, Android,
  hygiene, source marker, STG deploy contract, and final report consistency
  gates all passed.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-doc-hygiene-focused/report.md`.
- OCI DB check was skipped because the tunnel was not open for this focused run.
  The latest full OCI-backed passing run remains
  `codex-resume-20260826-api-drift-full-oci`.
- STG inspection remains unchanged:
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Full OCI Refresh After Doc Hygiene

- Opened the workspace OCI tunnel and confirmed the tunnel database was
  reachable.
- Re-ran the full consolidated helper:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-full-oci-after-doc-hygiene tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all backend, vaccination procurement support, admin-web,
  contract validation, generated-client drift scope, migration guard, Android,
  hygiene, source marker, STG deploy contract, final report consistency, and OCI
  DB checks passed.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-full-oci-after-doc-hygiene/report.md`.
- STG inspection remains unchanged:
  `stg_images_match_head_commit=false`,
  `stg_proves_current_worktree=false`.

## 2026-08-26 Resume Checkpoint: Inventory Doc Focused Recheck

- Recomputed the current worktree status-line inventory:
  - `51` modified tracked files,
  - `20` untracked status entries.
- Updated the final/progress docs so untracked evidence is described as
  status-line entries, because screenshot/XML evidence directories are grouped
  by `git status --short`.
- Ran focused verification:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-inventory-doc-focused tools/dev/vaccine-inventory-director-verify.sh`.
- Result: exit `0`; all code/doc gates passed. OCI was skipped because the
  tunnel was intentionally closed after the prior full OCI-backed pass. The
  latest full OCI-backed passing run remains
  `codex-resume-20260826-full-oci-after-doc-hygiene`.

## 2026-08-26 Resume Checkpoint: Live STG Running Recheck

- Rechecked Cloud Run after the deployment activity started.
- Current live revisions:
  - API: `goatos-api-stg-00259-f24`,
  - admin-web: `goatos-admin-web-stg-00232-jf6`,
  - kernel-worker: `goatos-kernel-worker-stg-00227-7s6`.
- The API/admin revisions changed, but all inspected services still carry
  `commit_sha=a5a64b7bbff4` and image tags:
  - `backend:a5a64b7bbff4`,
  - `admin-web:a5a64b7bbff4`.
- Kernel worker still has `GOATOS_WORKER_STAGES_ENABLED=true`, but its revision
  and backend image remain unchanged.
- Result: STG is running, but it is not yet proving this dirty feature worktree;
  `stg_proves_current_worktree=false` remains the honest status.

## 2026-08-26 Resume Checkpoint: Branch-Local OCI Phone E2E

- Reframed the requested E2E target as branch-local services backed by the OCI
  tunnel DB, not live STG and not any local container/database path.
- Started the branch API locally on isolated port `8090` with the OCI
  `DATABASE_URL`; `/readyz` returned `204`.
- Verified OCI schema already had migration `000211_pc_care_inventory_vaccine`
  and both `pc_care_task_inventory_requirements` /
  `pc_care_task_proofs` tables.
- Seeded direct DB fixture `e2e-local-oci-20260826` into OCI:
  - vaccination date `2026-09-02`,
  - task date `2026-08-26`,
  - Coimbatore / Castro / `Parts 7-9`,
  - 35 real alive goats from the selected shed,
  - 10 ET+TT obligations,
  - 25 PPR obligations,
  - one `vaccination_drive_assignments` row.
- Ran the branch `cmd/kernel-worker` locally against the OCI DB with worker
  stages enabled. The log shows `pc-care-inventory-vaccine` completed with
  `tasks_created=1`, `assignees_inserted=1`, and requirements upserted.
- OCI DB readback proved task
  `e9a82501-5d6e-4dd5-9748-26b56c7dcc8b`:
  - category `inventory_vaccine`,
  - status/work state `open/scheduled`,
  - planned/due `2026-08-26`,
  - assigned to Chandrakant,
  - requirements `ET+TT=10`, `PPR=25`.
- Branch API checks on the local OCI-backed API:
  - Chandrakant/director sees current/carry `inventory_vaccine` items including
    the Castro fixture with `capture_mode=task_proof` and
    `stock_fridge_video`,
  - Amit/operator gets `0` `inventory_vaccine` worklist items,
  - CEO monitor sees the Castro fixture with Chandrakant and ET+TT/PPR stock
    requirements.
- Installed branch dev APKs on both Poco devices using tokens minted against
  the local OCI-backed branch API and `adb reverse tcp:8080 tcp:8090`.
- Captured phone evidence under
  `context/execution/vaccine-inventory-director-e2e-2026-08-26/local-oci-branch-resume/`:
  - `director-home.png/xml`: director lands on Preventive Care / Vaccine Stock
    with current/carry cards at top,
  - `director-castro-detail.png/xml`: Castro detail shows `ET+TT 10 doses`,
    `PPR 25 doses`, fridge stock proof Photo/Video controls, and disabled
    submit until proof,
  - `operator-home.png/xml`: operator lands on Vaccination and has no
    PC Care/Vaccine Stock card,
  - `director-after-photo-tap.png/xml`: camera preview opened for fridge stock
    proof.
- Fresh phone proof submission was not completed in this pass: the attempted
  shutter tap pulled the Android notification shade instead of capturing the
  photo. Existing backend/Android/API tests plus earlier branch evidence still
  cover proof upload, submit, and verifier handoff; this local-OCI phone pass
  newly strengthens the branch-local OCI director/operator/CEO E2E.

## 2026-08-26 Resume Checkpoint: Vaccination Stock UI Correction

- Corrected the director-facing inventory vaccine entry so the visible phone
  chrome is Vaccination-owned:
  - backend nav key `vaccination_stock`,
  - bottom-bar label `Stock`,
  - href `/vaccination/stock`,
  - visible only for `pc_director` principals with `pc_care.execute`.
- Kept `/pc/inventory-vaccine` hosted as a compatibility route, but it is no
  longer the published director bottom-bar entry.
- Removed the date/weekday strip from both:
  - the director Stock work queue (`Vaccination` / `Stock`), and
  - the actual vaccination drive list (`Vaccination sheds`).
- Focused automated verification passed:
  - `go test ./internal/workforce/app`,
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.AppStartDestinationTest' --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.core.designsystem.icon.MeshaIconsNavKeyTest'`.
- Re-ran physical Poco visual regression against the branch API on `8090`
  backed by the OCI tunnel DB:
  - director: `context/execution/vaccine-inventory-director-e2e-2026-08-26/local-oci-ui-correction/director-stock-home-after-grants.png`
    and `.xml`;
  - operator: `context/execution/vaccine-inventory-director-e2e-2026-08-26/local-oci-ui-correction/operator-vaccination-home.png`
    and `.xml`.
- Director XML confirms `Vaccination`, `Stock`, current/carry task cards with
  due dates and `Open`/`Delayed` statuses, and no `Preventive Care`, no
  `Vaccine Stock`, and no weekday labels.
- Operator XML confirms `Vaccination sheds`, `Drives`, and `Alerts` only, with
  no `Stock`, no `Preventive Care`, no `Vaccine Stock`, and no weekday labels.

## 2026-08-26 Resume Checkpoint: Proof Media Flow And Camera Flash

- Rechecked the vaccination inventory stock proof capture path:
  - stock photos/videos from `PcCareTaskViewModel.onRecordTaskProof` go through
    `CaptureRepository.captureReplacingLatest`;
  - the media processor creates compressed, overlay-burned artifacts for both
    photo and video before upload;
  - proof upload and PC-care task-proof registration are durable Room outbox
    rows in the same `pc-care:task:<task-id>` FIFO lane, so registration waits
    on the uploaded proof reference.
- Confirmed this means fridge stock evidence uses the current proof compression,
  audit overlay, Room outbox, and sync architecture; it is not a separate raw
  upload path.
- Added CameraX torch support to both in-app proof cameras:
  - video proof capture has a visible `Flash auto/on/off` control;
  - photo proof capture has the same flash cycle as an icon control beside the
    shutter;
  - `Auto` samples preview brightness and enables torch when the camera view is
    too dark;
  - camera release/retry turns torch off to avoid leaving the device light on.
- Added focused unit coverage for torch mode cycling, hardware gating, and
  low-light threshold behavior.
- Verification passed:
  - `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.capture.ProofTorchTest'`;
  - `./gradlew :app:assembleStgDebug`.
- Installed the STG debug APK on both connected Poco devices and launched
  `sg.mesha.goatos.stg`; recent logcat scan showed no fatal startup exceptions.
