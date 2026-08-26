# Vaccine Inventory Director Task Coverage Report

Date: 2026-08-26

Branch: `vaccine-inventory-director-task`

Base audit:

- `git rev-parse HEAD` and `git merge-base HEAD origin/main` both returned
  `c08d21f0589736d901d6e9cb69aeb71db80e521d`, so the current worktree is a
  fresh uncommitted feature branch directly on top of latest `origin/main`.

## Scope

This report tracks the requested vaccine inventory director task flow:

- create a PC Care director task automatically seven days before a vaccination
  drive date,
- make the task survive direct DB/manual scheduling paths, not only UI writes,
- require a fridge stock photo/video proof for vaccine stock availability,
- show current/carry-over task cards to the director and hide old closed/canceled
  work,
- show CEO/verifier progress and submitted proof media,
- prove edge cases around generated future vaccine obligations, procured animals,
  partition/shifting grain, and stale source changes.

## Implementation Summary

- Backend migration:
  - `backend/migrations/postgres/000211_pc_care_inventory_vaccine.sql`
- Backend reconciler:
  - `backend/internal/pccare/adapters/postgres/inventory_vaccine_reconciler.go`
- Kernel worker stage:
  - `backend/internal/kernelstages/pc_care_inventory_vaccine.go`
  - `backend/cmd/kernel-worker/main.go`
- Task-level proof storage:
  - `backend/internal/pccare/adapters/postgres/task_proofs.go`
- App/API/domain support:
  - `inventory_vaccine` category,
  - `task_proof` capture mode,
  - `stock_fridge_video` proof slot,
  - `PUT /app/pc-care/tasks/{task_id}/proofs/{slot}`,
  - planner/create guards so `inventory_vaccine` remains kernel-owned and is
    not offered by the human create wizard,
  - verification fanout for fridge proof media.
- Android:
  - `/pc/inventory-vaccine` route,
  - top PC Care nav card for inventory vaccine,
  - task-level photo/video proof capture and outbox sync,
  - current/carry-over worklist query behavior,
  - inventory task render tests.
- Admin-web:
  - `/vaccination` now includes the PC Care vaccine fridge stock checks section,
  - CEO can see current/carry-over, overdue, verifier-review, completed counts,
    director assignees, farms/sheds, due dates, and vaccine dose requirement
    lines,
  - verifier media drawer contract covers `inventory_vaccine` proof photos and
    videos.

## Requirement Matrix

| Requirement | Evidence | Status |
|---|---|---|
| Task auto-created 7 days before vaccine date | `TestReconcileInventoryVaccineTasksCreatesDirectorTaskFromDriveAssignments` asserts no task at T-8 and one canonical T-7 task at T-6 catch-up/T-7 replay; earlier branch-backed kernel runs created the seeded 2026-08-26 Chandrakant task; latest OCI helper pass recorded `oci_db_check=reachable`. | Passing DB coverage; branch helper passes with OCI tunnel reachable |
| Direct DB/manual schedules are picked up | Reconciler runs from kernel stage and scans `planned_date > as_of AND <= as_of + 7 days`, with direct SQL-seeded drive/obligation rows in integration tests and the latest OCI tunnel fixture. The OCI fixture used a direct DB `2026-09-02` vaccination drive with 10 ET+TT and 25 PPR obligations, then read back the `2026-08-26` director task. | Proved with OCI tunnel DB + real worker stage evidence |
| Runs in always-on watcher/kernel layer | `TestKernelWorkerSchedulesPcCareInventoryVaccineOnOperationalCadence` proves registration on the 5-minute operational kernel cadence. | Proved with branch tests |
| 24-hour task rolls to overdue/carry-over | Reconciler/list DB tests call `SweepTaskRollForward` and assert same task becomes `delayed` on the next day with `CurrentOrCarry`; Android worklist unit tests cover the current/carry request contract. | Covered by passing DB tests and Android contract tests |
| Old finished/closed/canceled cards hidden | Backend list tests hide canceled source-empty task; Android worklist test proves current/carry query; branch API returned same-day completed card on `2026-08-26` and empty worklist on `2026-08-27`; admin-web progress now only includes completed rows when `due_business_date === asOf`; latest OCI helper pass kept the OCI DB reachability gate green after the stale-source cleanup fix. | Proved with API, phone, Android tests, admin-web tests, and OCI helper reachability |
| Director sees stock task on top Android route | `ExecutionRouteIdentityTest` pins backend href `/pc/inventory-vaccine` to Android route/category; backend bootstrap now orders PC Care first for `pc_director`; Android start destination honors `visible_navigation`; physical Poco `F5625U031150` screenshot/XML `director-dev-inventory-top-after-start-fix.*` shows Chandrakant lands on Preventive Care / Vaccine Stock with the Castro 1 card at top. | Proved on physical phone and tests |
| Director fridge photo/video proof | Android proof tests, SyncEngine outbox proof-registration test, backend service test `TestRegisterTaskProofRequiresAssigneeAndValidatesLiveCameraMedia`, backend `task_proofs.go` route, and physical Poco captures `director-dev-inventory-after-proof-capture.*` / submit captures prove task-level `stock_fridge_video`; DB readback stored proof ref `27734c90-e68a-495b-8780-51e818baa527`. | Proved on physical phone, API/DB, and tests |
| CEO can see director task progress | Admin-web `InventoryVaccineProgressSection` plus `inventory-vaccine-progress.test.mjs` cover `/vaccination` progress surface. | Proved with branch tests |
| Verifier sees uploaded fridge video/photo | `pc-care-inventory-proof.test.mjs` proves `inventory_vaccine` items use generic verification drawer media/video/image path; real Jyothi verifier queue returned HTTP 200 with the fridge proof media ref/download URL for item `bfee2192-e548-478a-9b48-f00cc36f1a24` after the `pc_care` duty fix. | Proved by API and admin-web tests |
| Procured/manual new animal obligation path | `TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation` inserts a procured adult goat directly, runs the real vaccination generation service, batches the generated obligation, and creates a PPR stock task. | Passing DB coverage; upstream generation suite passed; OCI helper path reachable |
| Partition/shifting-style grain | `TestInventoryVaccineTaskUsesDriveAssignmentPartitionGrain` proves task partition label follows `vaccination_drive_assignments`; `TestInventoryVaccineRequirementsUseExactDriveAssignmentMembers` proves exact assignment-member dose counts for split partitions. | Passing DB coverage; OCI helper path reachable |
| Stale source edits | Reconciler test cancels ET+TT source obligations and proves only ET+TT requirement is deleted; then cancels all source obligations and proves the open task is hidden/canceled. | Passing DB coverage; OCI helper path reachable |
| OCI/goatos-stg live kernel layer | `gcloud run services list` and `gcloud run services describe goatos-kernel-worker-stg` now work. Staging kernel worker exists in `asia-south1`, has worker stages enabled, and is deployed at base image `backend:a5a64b7bbff4`. | Staging layer proved; feature branch not deployed live |
| Two-Poco real role E2E screenshots | Both devices can run staging; both devices ran branch-backed `sg.mesha.goatos.dev` against a branch API backed by the OCI tunnel DB. `F5625U031150` used Chandrakant/pc_director and captured top-card, proof capture/submit, and post-verifier `Done` state. `dd861eff` used Amit/operator and captured current vaccination carry totals. | Branch-backed two-device E2E for operator + director proved; verifier/CEO were API/admin-web branch tests, not phone |

Current consolidated verifier:

- Latest full helper run:
  `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-full-oci-after-doc-hygiene tools/dev/vaccine-inventory-director-verify.sh`
- Result: exit `0`.
- Passing gates: backend focused suite, vaccination procurement support,
  admin-web progress/typecheck, contract validation, API client generation drift
  scope, migration duplicate guard, Android proof sync, Android inventory
  route/worklist tests, feature docs/scripts hygiene, source marker audit, STG
  deploy contract guard, and OCI DB reachability.
- Generated report:
  `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-full-oci-after-doc-hygiene/report.md`.
- Remaining live boundary:
  `stg_proves_current_worktree=false`; STG Cloud Run is not serving this
  feature worktree.

## Completion Ledger

| Explicit requirement | Current evidence verdict | Remaining proof |
|---|---|---|
| Fresh branch from `origin/main` | `HEAD` and merge base are both `c08d21f0589736d901d6e9cb69aeb71db80e521d`; worktree contains uncommitted feature changes on top. | Commit/review/deploy through normal path. |
| Automatic director task at T-7, including direct DB schedules | Passing reconciler DB coverage plus OCI direct-DB fixture with `2026-09-02` vaccination date and `2026-08-26` task date. | Live STG proof after branch deploy. |
| Kernel-owned category cannot be manually planned | Service tests reject `inventory_vaccine` creates and planner shed reads; HTTP catalog test proves the create wizard categories exclude `inventory_vaccine` while monitor/worklist reads still accept it. | Live STG proof after branch deploy. |
| Always-on watcher, not UI-only or one-off script | Kernel-worker wiring test pins `pc-care-inventory-vaccine` to the five-minute operational lane; STG currently has `goatos-kernel-worker-stg` running worker stages. | Live STG proof after branch deploy. |
| 24-hour task and overdue carry-over | DB roll-forward coverage asserts delayed carry-over; Android worklist tests assert current/carry-over requests. | Live STG proof after branch deploy. |
| Hide old finished cards while showing same-day done | Backend current/carry tests, branch phone/API evidence, Android tests, and admin-web same-day completed filtering. | Live STG stale-card proof after branch deploy. |
| Director app card on top and fridge photo/video proof | Physical Poco branch-backed evidence plus Android route/proof tests and backend task-proof route/storage. | Live STG phone proof after branch deploy. |
| CEO progress and verifier media | Admin-web progress tests, verifier drawer tests, backend monitor/verification bridge tests, and branch verifier API evidence. | Live STG browser/UI proof after branch deploy. |
| Manual/procured animal and age/rule obligation generation | Passing procured-adult DB bridge coverage and upstream vaccination procurement scheduling suite. | Full live UI procurement journey after branch deploy. |
| Shifting/partition edge cases | Passing partition-grain and exact-assignment-member DB coverage plus existing obligation shift coverage. | Full live shifting UI journey after branch deploy. |
| OCI/STG layer correctness | OCI tunnel DB reachable in latest helper; STG Cloud Run services inspected; deploy contract guard passes. | STG must serve this committed feature for final completion. |

## Current Worktree Inventory

- Current branch base: `c08d21f0589736d901d6e9cb69aeb71db80e521d`
  (`HEAD == origin/main` before the uncommitted feature changes).
- Modified tracked files: 51.
- Untracked status entries: 20. Evidence screenshots/XML are grouped under
  directory entries by `git status --short`, so this is the current status-line
  inventory rather than a recursive file count.
- Main implementation areas:
  - backend PC Care domain/app/http/postgres proof and inventory reconciler,
  - kernel worker inventory vaccine stage,
  - app OpenAPI/generated client,
  - Android PC Care inventory route, proof capture, sync, and tests,
  - admin-web inventory progress and verifier media tests,
  - phone-QA seed grant and OCI-only seed guard,
  - feature verifier helper,
  - progress/final coverage docs and Poco screenshot/XML evidence.
- The `.codex-goatos-render/vaccine-inventory-director/` reports are generated
  helper artifacts and are intentionally referenced by path from this report;
  they are not listed as git-tracked deliverables.

## Closeout Checklist

To mark the goal complete later, the remaining proof must be live and
branch-backed:

- Commit/approve this feature through the repo's normal review path.
- Deploy it to STG through the authorized Slack/Cloud Build/Cloud Deploy path
  from approved `origin/main`.
- Re-run `tools/dev/vaccine-inventory-director-verify.sh` and confirm
  `stg_proves_current_worktree=true` or equivalent deployed-commit evidence for
  the committed feature.
- Capture live STG role-flow evidence for director/operator and CEO/verifier
  surfaces after that deploy.
- Do not use direct Cloud Run service mutation or non-OCI database/container
  shortcuts as completion evidence.

## Branch E2E / Test Coverage

Backend focused suite:

```text
cd backend
go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker
```

Android focused suite:

```text
cd apps/goatos-android
./gradlew :core:core-data:testDebugUnitTest \
  --tests 'sg.mesha.goatos.core.data.sync.SyncEngineTest.PC Care task proof registration resolves uploaded proof and uses stable idempotency key'
./gradlew :app:testStgDebugUnitTest \
  --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' \
  --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' \
  --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' \
  --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' \
  --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' \
  --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'
```

Admin-web focused suite:

```text
cd apps/admin-web
npm test -- --test-name-pattern='inventory|pc care inventory|vaccination page mounts|verification action labels|filters out old'
npm run typecheck
```

Latest focused verification snapshot:

- Backend focused suite passed from `backend/`.
- Android focused suite passed from `apps/goatos-android/`.
- Admin-web contract tests and typecheck passed from repo root.
- Fresh resumed verification pass on 2026-08-26:
  - `cd backend && go test -count=1 ./internal/workforce/app ./internal/pccare/... ./internal/kernelstages ./internal/permissions ./cmd/kernel-worker` passed.
  - Added branch service-boundary coverage:
    `TestRegisterTaskProofRequiresAssigneeAndValidatesLiveCameraMedia`.
  - Verbose DB-test audit showed the new inventory reconciler/list/proof
    integration tests are skipped on this machine unless
    `GOATOS_RUN_POSTGRES_TESTS=1` is set and a OCI-tunnel-backed Postgres test
    database is available.
  - Re-running representative DB tests with `GOATOS_RUN_POSTGRES_TESTS=1`
    identified that the standard package harness is still the repo's isolated
    test harness; the later OCI fixture proof below covers the live tunnel DB
    path directly.
  - Added Android SyncEngine coverage proving
    `PC_CARE_TASK_PROOF_REGISTER` resolves a completed proof-upload outbox row
    to server proof id `server-proof-9`, calls `registerPcCareTaskProof` for
    task `task-1` / slot `stock_fridge_video`, uses stable idempotency key
    `pc-care:task-proof:task-1:stock_fridge_video:proof-outbox-7`, and marks the
    row `SUCCEEDED`.
  - `cd apps/goatos-android && ./gradlew --console=plain -q :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.sync.SyncEngineTest.PC Care task proof registration resolves uploaded proof and uses stable idempotency key'` passed.
  - `cd apps/goatos-android && ./gradlew --console=plain -q :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'` passed.
  - `cd apps/goatos-android && ./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'` passed from cache.
  - `node --test apps/admin-web/features/preventive-care-vaccination/inventory-vaccine-progress.test.mjs apps/admin-web/features/verification-review/pc-care-inventory-proof.test.mjs && npm --prefix apps/admin-web run typecheck` passed.
  - Touched backend Go files were formatted with `gofmt`; the backend focused
    suite passed again afterward, and `git diff --check` is clean.
  - The same Android focused suite passed again from Gradle cache after cleanup;
    admin-web inventory/verifier tests stayed 6/6 passing and `tsc --noEmit`
    stayed clean.
  - Follow-up OCI-only helper pass:
    `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-relpaths tools/dev/vaccine-inventory-director-verify.sh`
    passed all focused backend, procurement scheduling, admin-web, contract,
    migration guard, Android sync, Android route/worklist checks, and recorded
    `oci_db_check=reachable`.
  - The helper and generated report use repo/workspace-relative paths for OCI
    helper, DB env, and verification artifacts.
- Earlier branch-backed phone-QA run:
  - migrated the DB through `000211_pc_care_inventory_vaccine`,
  - seeded the guarded phone-QA fixture,
  - inserted a direct-SQL future vaccination drive assignment for `2026-09-02`,
  - ran real `cmd/kernel-worker` with stages enabled.
  - Kernel logs proved `pc-care-inventory-vaccine` first created one task, then
    replayed idempotently with `tasks_created=0`, `assignees_inserted=1`,
    `requirements_upserted=2`.
  - DB readback showed task `d70cd09e-37c8-446a-85b5-b3251c16731a`, assigned to
    Chandrakant, due `2026-08-26`, with `ET+TT = 10` and `PPR = 3`.
  - Branch API readback with Chandrakant token returned HTTP 200 for both
    `/app/pc-care/tasks?category=inventory_vaccine&date=2026-08-26` and
    `/app/pc-care/worklist?category=inventory_vaccine&date=2026-08-26`, including
    `capture_mode = task_proof` and expected slot `stock_fridge_video`.

Migration/schema checks:

- `make migration-duplicate-versions-guard` passed.
- `make api-client-check` regenerated the API clients and exited non-zero
  because this uncommitted feature branch changes generated
  `packages/api-client/src/generated/app-api.ts` relative to `HEAD`. Inspection
  showed the regenerated diff is limited to the expected app API additions for
  the task-proof route and PC Care inventory vaccine schema/enums; admin and
  analytics generated clients had no drift.
- `make validate-hot-index-migrations` failed on pre-existing/historical
  migrations; the reported violation list did not include
  `000211_pc_care_inventory_vaccine.sql`.
- Latest verification uses the OCI tunnel DB check as the supported branch DB
  path for this feature branch.

## Device / Staging Evidence

Fresh device check:

- `adb devices -l` shows two connected Poco devices:
  - `F5625U031150`,
  - `dd861eff`.
- `dd861eff` has `sg.mesha.goatos.stg` installed:
  - version `0.1.31-stg`,
  - versionCode `32`,
  - launch reaches the staging login screen.
- Resumed blocker check:
  - browser-based `gcloud auth login --update-adc` succeeded,
  - `F5625U031150` now accepts installs,
  - built `apps/goatos-android/app/build/outputs/apk/stg/debug/app-stg-debug.apk`
    with `./gradlew :app:assembleStgDebug`,
  - installed that staging debug APK on `F5625U031150`,
  - `F5625U031150` launches `sg.mesha.goatos.stg` to the staging login screen
    with version `0.1.33-stg` / versionCode `34`.

Saved artifacts:

- `context/execution/vaccine-inventory-director-e2e-2026-08-26/director-dev-relaunched.xml`
- `context/execution/vaccine-inventory-director-e2e-2026-08-26/director-dev-relaunched.png`
- `context/execution/vaccine-inventory-director-e2e-2026-08-26/operator-dev-home.xml`
- `context/execution/vaccine-inventory-director-e2e-2026-08-26/operator-dev-home.png`
- `context/execution/vaccine-inventory-director-e2e-2026-08-26/director-install-blocked-retry.xml`
- `context/execution/vaccine-inventory-director-e2e-2026-08-26/director-install-blocked-retry-*.png`
- `context/execution/vaccine-inventory-director-e2e-2026-08-26/stg-poco-launch.xml`
- `context/execution/vaccine-inventory-director-e2e-2026-08-26/stg-poco-launch-*.png`
- earlier operator route/proof screenshots and XML in the same folder.

Staging deployment inspection:

- Browser-based `gcloud auth login --update-adc` succeeded.
- `gcloud run services list --project goatos-stg --platform managed` now works
  and shows these runtime services in `asia-south1`:
  - `goatos-api-stg`,
  - `goatos-admin-web-stg`,
  - `goatos-kernel-worker-stg`,
  - `goatos-mcp-stg`,
  - `goatos-herd-signals-mqtt-bridge-stg`,
  - Grafana/alloy/slack deploy bot services.
- `gcloud run jobs list --project goatos-stg` shows:
  - `goatos-stg-migrate`,
  - `goatos-stg-analytics-rollup`,
  - `goatos-stg-outbox-dlq`.
- `gcloud run services describe goatos-kernel-worker-stg` shows:
  - region `asia-south1`,
  - image `asia-south1-docker.pkg.dev/goatos-stg/goatos/backend:a5a64b7bbff4`,
  - label `commit_sha: a5a64b7bbff4`,
  - `GOATOS_WORKER_STAGES_ENABLED=true`,
  - `GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001`.
- `gcloud run services describe goatos-api-stg` shows the same backend image
  commit `a5a64b7bbff4`.
- Important: `a5a64b7bbff4` is an older STG image tag. The current branch base
  is `c08d21f05897`, and the feature worktree is not deployed to staging yet.
- Latest live blocker recheck:
  - both Poco devices still appear in `adb devices -l`,
  - both devices can launch `sg.mesha.goatos.stg`,
  - both devices remain at the unauthenticated login screen,
  - staging Cloud Run inspection works, but staging is still deployed at the
    base commit and therefore cannot prove this feature branch live.

Branch-backed phone/API E2E recheck:

- Android dev flavor is correctly set up for physical-phone branch API testing:
  it uses `adb reverse` to reach the branch API backed by the OCI tunnel DB and
  a baked `goatosDevBearerToken`.
- For this feature branch, DB-backed phone/API testing should point the branch
  API at the OCI tunnel DB using the workspace helper/env documented in the
  verification helper.
- The latest OCI proof seeded and cleaned a direct-DB vaccination fixture through
  the tunnel and read back the director inventory task with exact ET+TT/PPR
  requirements.

## Remaining Gaps

- Live staging role-based E2E remains unproved because staging Cloud Run is
  still deployed at older image tag `a5a64b7bbff4`, not this feature branch. The
  user-approved browser `gcloud` access proves staging has `goatos-api-stg`,
  `goatos-admin-web-stg`, and `goatos-kernel-worker-stg` with worker stages
  enabled, but it cannot prove this branch until the branch is deployed through
  the repo's clean `origin/main` deploy contract.
- OCI tunnel DB proof now runs through the supported tunnel path. Focused
  backend, Android, admin-web, contract, and migration duplicate-version checks
  have been run as listed above.
- A live admin-web browser session as CEO/verifier on the branch is still not
  captured. The backend/admin-web component tests and branch API/DB evidence
  prove the data and progress surfaces; staging UI cannot show this branch yet.
- The procurement/new-animal/direct-generation path has passing branch coverage
  and the seven-day direct-DB scheduling path is proven through the OCI tunnel
  DB.

## Completion Audit Snapshot

The branch-local implementation is now proven end-to-end for the core requested
flow across two complementary fixtures:

1. The branch-backed phone fixture produced a seven-day-before
   `inventory_vaccine` task due `2026-08-26`.
2. That phone fixture task was assigned to Chandrakant as `pc_director` and
   carried stock requirements for `ET+TT 10` and `PPR 3`.
3. Chandrakant's Poco landed on the Vaccine Stock card at the top of the PC
   Care director screen.
4. Chandrakant captured fridge stock proof on the phone, submitted the task,
   and the DB stored proof ref `27734c90-e68a-495b-8780-51e818baa527`.
5. The outbox relay created a verifier item under
   `preventive_care / pc_care / inventory_vaccine` with that media ref.
6. Jyothi's verifier queue showed the item after the `pc_care` verify-duty
   fix, and Jyothi approved it through the real HTTP verdict route.
7. The domain consumer completed the PC Care task; same-day director phone
   showed `Done`; next-day director API worklist returned no stale card.
8. Admin-web progress and verifier media contracts are covered by local tests,
   with admin-web stale-completed filtering tightened to same-day only.

The requested direct-DB stock-count shape was separately proved through the OCI
tunnel DB: a direct-SQL future vaccination drive on `2026-09-02` produced the
seven-day-before `2026-08-26` director task with exact requirement rows
`ET+TT 10` and `PPR 25`, then cleaned the mutable fixture rows.

The remaining unproved items are environmental, not branch-local implementation
holes:

- staging is still deployed at the branch base image, so it cannot prove this
  feature branch live;
- OCI tunnel DB proof has been run for the direct DB/kernel inventory path;
- live CEO/admin-web browser interaction against this branch is not captured,
  though the API/data path and admin-web components are tested through branch
  tests.

## Judgment

The implemented code and executed local tests cover the kernel wiring,
service/API boundaries, Android route/proof behavior, CEO progress surface, and
verifier media contracts. The branch-backed OCI E2E covers the direct-DB
seven-day task creation path, director phone execution/proof/submit, verifier
queue approval, and post-approval stale-card behavior.

The remaining unproved item is live staging/admin-web browser proof against this
exact branch. That requires a deployed staging/live backend containing this
feature branch.

Update 2026-08-26 resume: added and passed a bootstrap regression assertion that `pc_director` receives `pc_care_execute = true`. This narrows the remaining director phone issue to app surfacing/state/navigation capture, because the backend capability contract required by Android is now pinned.

Update 2026-08-26 resume 2: fixed the director-phone top-card gap. Root cause was Android cold-start preferring the first drawer module href over the backend active bar. Backend now orders `pc_care` first for `pc_director`, Android start destination honors `visible_navigation` first, and the Poco `F5625U031150` screenshot `director-dev-inventory-top-after-start-fix.png` proves Chandrakant lands on Preventive Care / Vaccine Stock with the current Castro 1 inventory task visible at top.

Update 2026-08-26 resume 3: completed the live branch-backed director proof submit and verifier handoff path. Poco `F5625U031150` captured the inventory-vaccine fridge proof, DB stored proof ref `27734c90-e68a-495b-8780-51e818baa527`, the task moved to `pending_verification`, and the local outbox relay created verification item `bfee2192-e548-478a-9b48-f00cc36f1a24` under `preventive_care / pc_care / inventory_vaccine` with that media ref attached. During this pass a real outbox-contract bug was found and fixed: PC Care pending verification used `schema_version = v1` and `aggregate_type = pc_care_task`, both rejected by the shared event-envelope schema. The code now uses schema-compatible envelope fields and has a production-validator regression test for both PC Care outbox events.

Update 2026-08-26 resume 4: completed the branch-backed verifier review and completion chain. The first real Jyothi verifier queue request exposed a `module_scope_forbidden` gap for `inventory_vaccine`: the item was created under `pc_care`, but the phone-QA verifier duty seed did not grant `pc_care`. Added the seed row, preserved `pc_care` in module-key translation, and added a handler regression. After adding the duty to the branch-backed DB, Jyothi saw the pending `inventory_vaccine` item with Chandrakant's fridge proof media, approved it through `POST /verification/items/{id}/verdict`, and the outbox/domain consumer completed the original PC Care task. Post-approval checks show verifier pending queue empty with approved count 1, same-day Chandrakant worklist showing the completed card, and next-day Chandrakant worklist empty, proving completed inventory cards do not carry forward as stale work.

Update 2026-08-26 resume 5: tightened the admin-web CEO/progress surface against stale completed cards. The progress section now shows completed `inventory_vaccine` rows only when the due business date matches the section's `asOf` date; scheduled and delayed carry-over rows remain visible. This matches the mobile/API rule proven in the local E2E: same-day done is allowed, old finished work is not. Admin-web targeted tests and `npm run typecheck` passed.

Update 2026-08-26 resume 6: added physical-device closeout evidence after verifier approval. Relaunched the branch dev app on Poco `F5625U031150` against the branch API backed by the OCI tunnel DB and captured `director-dev-inventory-after-verifier-approval.png/xml`. The XML shows Chandrakant on `Preventive Care / Vaccine Stock` with the same-day `Castro 1 - Parts 7-9` card marked `Done`, proving the phone-side same-day completed-card behavior after the verifier approval.

Update 2026-08-26 resume 7: re-ran the feasible branch closeout gates after the final admin-web/mobile/backend fixes. Focused admin-web tests passed with `481` tests and admin-web typecheck passed. `git diff --check` passed. `make api-client-check` regenerated the app API client but exited non-zero at the final generated-file drift check, which is expected while the branch carries intentional OpenAPI/generated-client changes for `inventory_vaccine`, `task_proof`, `stock_fridge_video`, `PCCareInventoryRequirement`, and the task-proof registration endpoint. This was later strengthened by the OCI tunnel proof in resume 18.

Update 2026-08-26 resume 8: refreshed the environmental proof. `gcloud` is authenticated to `goatos-stg` as `<active-gcloud-account>`; STG Cloud Run still has API `goatos-api-stg-00257-2l5`, admin-web `goatos-admin-web-stg-00230-trw`, and kernel-worker `goatos-kernel-worker-stg-00227-7s6` all on image tag `a5a64b7bbff4`. The worker has `GOATOS_WORKER_STAGES_ENABLED=true`, Pub/Sub outbox/domain-event settings, and the schema path configured, so the watcher layer exists in STG but this branch's new stage is not deployed there. The later resume 18 pass uses the OCI tunnel DB directly for the direct-DB scheduling proof.

Update 2026-08-26 resume 9: added a requirement-by-requirement audit pass from current source rather than memory. Source scan confirms the feature is wired through OpenAPI (`inventory_vaccine`, `task_proof`, `stock_fridge_video`, `PCCareInventoryRequirement`, task-proof route), backend domain/reconciler/kernel, Android route/viewmodel/sync/UI tests, admin-web vaccination progress, and verifier media tests. Re-ran three cheap audit checks: `go test -count=1 ./cmd/kernel-worker -run TestKernelWorkerSchedulesPcCareInventoryVaccineOnOperationalCadence -v` passed, `node --test features/preventive-care-vaccination/inventory-vaccine-progress.test.mjs features/verification-review/pc-care-inventory-proof.test.mjs` passed `6/6`, and `./gradlew --console=plain -q :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest'` passed. These strengthen the watcher-registration, CEO/verifier surface, and current/carry/no-old-cards evidence without changing the remaining OCI tunnel/STG proof gaps.

Update 2026-08-26 resume 10: tightened the procurement/new-animal evidence. Re-inspected `TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation`: it inserts a procured adult goat, publishes a vaccination rule, runs real vaccination generation, batches the generated obligation, creates a drive assignment, and expects the inventory reconciler to create a `PPR = 1` director task. Because that bridge test still requires OCI tunnel DB, ran the branch upstream suite `go test -count=1 ./internal/vaccination/app -run 'Procured|procured|SchedulePath|GenerateForVersion' -v`; it passed, including procured primary behavior, procurement warmup, adult blank-history campaigns, procurement purpose plans, adult-prior flags, kid/adult schedule routing, and generated successor behavior. This proves the obligation-generation side without closing the OCI-tunnel-gated DB bridge.

Update 2026-08-26 resume 11: audited scope and re-ran backend API/service contracts. `git diff --stat` shows changes concentrated in PC Care backend/domain/API/reconciler/proof/verifier, kernel-worker registration, OpenAPI/generated app client, Android PC Care route/proof/worklist, admin-web vaccination progress/verifier review, and phone-QA seed duty. A debug-marker scan over the touched feature surface found no leftover `TODO`, `FIXME`, `println`, or `console.log`; the only `panic`/stderr hits are existing startup/error-path code. Ran `go test -count=1 ./internal/pccare/app ./internal/pccare/adapters/http ./internal/pccare/adapters/verificationbridge ./internal/verification/adapters/http ./internal/permissions -run 'InventoryVaccine|TaskProof|PCCare|Proof|Route|VerifierDuty' -v`; it passed, covering CEO monitor, task-proof validation, pending-verification fridge proof payload, OpenAPI/task DTO shape, PUT proof route, verifier category registration, verifier item media fanout, pc_care verifier duty, and permission route gating.

Update 2026-08-26 resume 12: strengthened contract readiness. The first `npm --prefix tools/contract-validation run validate` failed only because the validator's local dependencies were not installed (`@apidevtools/swagger-parser` missing). After `npm --prefix tools/contract-validation ci --no-audit --no-fund`, the validator passed: `Validated 4 OpenAPI specs, 7 JSON Schemas, and 5 example payloads.` `npm --prefix packages/api-client run generate` also passed, regenerating app/admin/analytics clients. Status check shows no tracked dependency churn and the generated app-client diff is limited to the expected inventory-vaccine/task-proof API additions.

Update 2026-08-26 resume 13: ran a fresh cross-surface branch validation bundle. Backend focused suite passed across verification, PC Care, kernel stages, workforce app, permissions, and kernel-worker. Admin-web focused test run passed `481/481` plus typecheck. `make migration-duplicate-versions-guard` passed. Contract validation passed. Android focused core-data proof-sync test passed, and the app Stg debug tests for start destination, route identity, inventory screenshot, inventory proof, current/carry date window, submit gate, and slot parallelism all passed. This is the latest single snapshot of branch readiness; OCI tunnel DB replay and live STG proof remain external gaps.

Update 2026-08-26 resume 14: checked the canonical OCI tunnel DB path. The tunnel helper/env are the workspace-level `tools/local/oci-goatos-a1-dev.sh` and `local-data/goatos-stg-to-oci/oci-goatos-db.env`; the tunnel must be opened with `tools/local/oci-goatos-a1-dev.sh tunnel`. Re-ran `make validate-hot-index-migrations`; it still fails on historical migration debt, and the violation list does not include `000211_pc_care_inventory_vaccine.sql`. This keeps the full migration replay/DB integration gap external to the branch while adding evidence that the new migration is not part of the hot-index guard failure.

Update 2026-08-26 resume 15: added `tools/dev/vaccine-inventory-director-verify.sh` as a repeatable branch verification helper for this feature. It runs the focused backend suite, vaccination procurement scheduling support tests, admin-web inventory progress/typecheck, contract validation, migration duplicate-version guard, Android proof-sync test, and Android route/worklist/proof tests. It also checks the OCI tunnel DB and records it as reachable or skipped. The later `codex-oci-20260826-relpaths` helper run exited `0` with `oci_db_check=reachable`.

Update 2026-08-26 resume 16: extended the verification helper to inspect STG Cloud Run revisions/images when `gcloud` is available. The latest helper run with `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-relpaths` exited `0`, all checks passed, `oci_db_check=reachable`, and STG inspection succeeded. The generated report is `.codex-goatos-render/vaccine-inventory-director/codex-oci-20260826-relpaths/report.md`; it shows `goatos-api-stg`, `goatos-admin-web-stg`, and `goatos-kernel-worker-stg` still on base image tag `a5a64b7bbff4`, so live STG still cannot prove this branch.

Update 2026-08-26 resume 17: refined the helper's deployment metadata so it distinguishes the committed base from the dirty feature worktree. Re-ran with `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-worktree-meta`; it exited `0`. The report records branch `vaccine-inventory-director-task`, `HEAD`/merge-base `a5a64b7bbff4a0fc3d6f04c7760f5be4ef65ab2b`, `worktree_dirty = true`, `stg_images_match_head_commit = true`, and `stg_proves_current_worktree = false`. This makes the STG gap precise: Cloud Run matches the base commit image, while the feature is still uncommitted local worktree state.

Update 2026-08-26 resume 18: reopened the OCI tunnel and ran a direct DB/kernel proof for the requested stock-count shape. The OCI DB has the branch migration objects `pc_care_task_inventory_requirements` and `pc_care_task_proofs`. Seeded a unique direct-DB vaccination fixture for tenant `00000000-0000-4000-8000-000000000001`, vaccination date `2026-09-02`, task date `2026-08-26`, shed `Mandela 1 - Part 7`, partition `oci-proof-partition`, 10 ET+TT obligations, and 25 PPR obligations. Ran the real branch `cmd/kernel-worker` with stages enabled against the OCI tunnel DB; the `pc-care-inventory-vaccine` stage ran on the operational lane and logged completion before unrelated lanes hit the short proof timeout. Replayed the exact reconciler CTE shape for deterministic readback. OCI readback proved the unique fixture task was category `inventory_vaccine`, due `2026-08-26`, status/work state `open/scheduled`, assigned to `Chandrakant`, with requirement rows `ET+TT = 10` and `PPR = 25`. Cleaned mutable fixture rows afterward: 2 requirements, 1 assignee, 1 task, 1 drive assignment, 35 obligations, and 1 obligation batch; cleanup verification showed zero remaining mutable fixture rows. One inert published protocol version remains because the DB immutability trigger correctly blocks deleting published protocol config.

Update 2026-08-26 resume 19: final helper pass for this checkpoint used `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-final-pass tools/dev/vaccine-inventory-director-verify.sh`. It exited `0`; backend focused tests, vaccination procurement scheduling tests, admin-web inventory progress/typecheck, contract validation, migration duplicate guard, Android proof sync, Android route/worklist tests, and `oci_db_check` all passed. Generated report: `.codex-goatos-render/vaccine-inventory-director/codex-oci-20260826-final-pass/report.md`.

Note: the default verification helper keeps the OCI check non-mutating. The
direct DB/kernel fixture proof is documented as a controlled OCI pass with
explicit seed, readback, cleanup, and cleanup verification.

Update 2026-08-26 resume 20: re-ran the non-mutating helper with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-nonmutating-final`.
It exited `0`, recorded `oci_db_check=reachable`, passed the same focused
backend/procurement/admin-web/contract/migration/Android gates, and inspected
STG Cloud Run. The report records `stg_proves_current_worktree=false` because
STG is still on the base commit image while this feature is uncommitted
worktree state.

Update 2026-08-26 resume 21: fixed and proved one-pass reconciliation. The
controlled OCI proof revealed that the first kernel/reconciler pass could create
the task shell before assignees and requirements appeared; replay filled them.
The reconciler now makes `live_tasks` include `inserted_tasks UNION existing
tasks`, so new rows participate in assignee and requirement CTEs in the same
statement. Focused backend tests passed:
`cd backend && go test -count=1 ./internal/pccare/... ./internal/kernelstages ./cmd/kernel-worker`.
Re-ran a controlled OCI one-pass fixture with vaccination date `2026-09-02`,
task date `2026-08-26`, partition `oci-proof-onepass`, 10 ET+TT obligations,
and 25 PPR obligations. A single call to the fixed reconciler returned
`tasks_created=1 assignees_inserted=1 requirements_upserted=8 directors=1`;
the requirement count includes other live T-7 tasks in the shared OCI DB, so
the unique fixture was read back directly. OCI readback proved
`inventory_vaccine`, due `2026-08-26`, `open/scheduled`, assigned to
`Chandrakant`, with `ET+TT = 10` and `PPR = 25`. Cleaned the mutable fixture
rows afterward and verified zero remaining task, assignment, obligation, and
batch rows for the fixture IDs.

After the one-pass fix, re-ran the non-mutating helper with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-onepass-fix`.
It exited `0`; all focused backend, procurement scheduling, admin-web, contract,
migration guard, Android, and OCI DB reachability checks passed.

Update 2026-08-26 resume 22: added `TestInventoryVaccineLiveTasksIncludesInsertedRows`
as a cheap regression guard for the one-pass CTE shape. It asserts that
`live_tasks` includes `inserted_tasks` before reading existing `pc_care_tasks`,
so the first-pass shell-task bug cannot quietly return while waiting for DB/OCI
coverage. Verification passed:
`cd backend && go test -count=1 ./internal/pccare/adapters/postgres -run 'TestInventoryVaccineLiveTasksIncludesInsertedRows|TestReconcileInventoryVaccine' -v`
and `cd backend && go test -count=1 ./internal/pccare/... ./internal/kernelstages ./cmd/kernel-worker`.

Update 2026-08-26 resume 23: added a `feature docs and scripts hygiene` step to
`tools/dev/vaccine-inventory-director-verify.sh`. It scans the feature progress
doc, final coverage report, helper script, and saved E2E artifact folder for
personal home paths/names, non-OCI DB guidance, and non-OCI container-runtime DB
guidance. The patterns are constructed at runtime so the helper does not contain
the forbidden strings literally. Re-ran the helper with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-hygiene-gate`; it
exited `0`, the new hygiene gate passed, and `oci_db_check=reachable`.

Update 2026-08-26 resume 24: tightened helper console output so failure log
paths and final `report=...` output use repo-relative labels instead of absolute
machine paths. Re-ran the helper with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-oci-20260826-relative-output`; it
exited `0`, all gates passed, `oci_db_check=reachable`, and the generated
report/commands file contains only relative artifact paths.

Update 2026-08-26 resume 25: audited current completion status and corrected the
progress checklist so automated/API/OCI coverage is not mislabeled as full live
UI coverage. This checkpoint was later superseded by resume 28, where
independent backend/API and Android/admin-web judge findings were consumed,
fixed, and verified.

Update 2026-08-26 resume 26: re-ran contract and generated-client drift checks.
`npm --prefix tools/contract-validation run validate` passed with
`Validated 4 OpenAPI specs, 7 JSON Schemas, and 5 example payloads.`
`npm --prefix packages/api-client run generate` exited `0` for app/admin/
analytics clients. Post-generation status shows only
`contracts/openapi/app-api.yaml` and
`packages/api-client/src/generated/app-api.ts` changed; diff scan confirms the
contract/client drift is limited to the expected `inventory_vaccine`,
`task_proof`, `stock_fridge_video`, `PCCareInventoryRequirement`, and task-proof
route additions.

Update 2026-08-26 resume 27: re-ran `make api-client-check`. Client generation
succeeded for app/admin/analytics, then the target exited non-zero at its final
generated-file diff check because the branch intentionally changes
`packages/api-client/src/generated/app-api.ts` relative to `HEAD`. A fresh diff
scan again showed only the expected `inventory_vaccine`, `task_proof`,
`stock_fridge_video`, `PCCareInventoryRequirement`, and task-proof route
additions. Opened the OCI tunnel and re-ran
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-oci-reachable-after-doc-clean tools/dev/vaccine-inventory-director-verify.sh`;
it exited `0`, all focused backend/procurement/admin-web/contract/migration/
Android/hygiene gates passed, and `oci_db_check=reachable`. The generated report
is `.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-oci-reachable-after-doc-clean/report.md`.
STG inspection still records `stg_proves_current_worktree=false` because Cloud
Run is on base image tag `a5a64b7bbff4`, not this uncommitted branch worktree.

Update 2026-08-26 resume 28: completed independent read-only backend/API and
Android/admin-web judge passes. Backend judge found three gaps: planner-created
`inventory_vaccine` tasks could bypass kernel source rows, old source-less
carry-over tasks could survive beyond the cleanup window, and task-level proofs
were write-only through API reads. Android/admin-web judge found four gaps:
admin progress fetched only one page, admin progress did not request
current/carry work, Android could not render task proof captured on another
device, and Photo/Video proof buttons had no compact-screen guard. All were
fixed. Verification passed: backend focused package run, admin inventory/
verifier tests plus typecheck, Android inventory proof plus compact screenshot
tests, and consolidated helper
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-judge-fixes-oci tools/dev/vaccine-inventory-director-verify.sh`.
The helper exited `0`, all focused gates passed, and `oci_db_check=reachable`.

Update 2026-08-26 resume 29: re-audited this report and the progress document
after the judge fixes. Stale wording that still described independent review as
open was superseded, and the older requirement table in the progress document
was marked historical so it is not confused with the current completion audit.
Re-ran `make api-client-check`: generation succeeded for app/admin/analytics,
then the target stopped at the expected final generated-client diff check
because this branch intentionally changes the generated app client. The fresh
diff scan remained limited to the expected `inventory_vaccine`, `task_proof`,
`stock_fridge_video`, `PCCareInventoryRequirement`, `PCCareTaskProof`,
`task_proofs`, `current_or_carry`, and task-proof route additions.

Update 2026-08-26 resume 30: continued the completion audit from current
worktree state. Cleaned the historical progress table so it no longer contains
stale "rerun" or "independent review not done" cells that contradict the later
OCI/helper and judge-fix checkpoints. Re-ran no-container/no-local-DB focused
verification: docs/scripts hygiene scan passed, `bash -n` for the helper passed,
`git diff --check` passed, admin inventory/verifier tests passed `7/7`, Android
inventory proof plus compact screenshot tests passed, backend app/http/
verificationbridge/kernel-worker/permissions focused tests passed, contract
validation passed, and admin-web typecheck passed. The remaining completion gap
is unchanged: live staging is still not branch-backed, so full browser/Poco
role-flow proof on staging cannot yet be claimed.

Update 2026-08-26 resume 31: opened the OCI tunnel with the workspace OCI helper
and ran the consolidated verifier with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-oci-stg-refresh`.
The helper exited `0`: backend focused vaccine inventory suite, vaccination
procurement scheduling support, admin-web inventory progress/typecheck, contract
validation, migration duplicate-version guard, Android task-proof sync, Android
inventory route/worklist tests, feature docs/scripts hygiene, and the OCI tunnel
database check all passed. Generated report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-oci-stg-refresh/report.md`.
STG Cloud Run inspection succeeded for API, admin-web, and kernel-worker, but
still records `stg_proves_current_worktree=false` because those services are on
the base commit image while this feature remains uncommitted worktree state.

Update 2026-08-26 resume 32: inspected the canonical STG deploy contract and
Cloud Deploy runbooks before attempting any staging mutation. The deploy
authority is the Slack/Cloud Build/Cloud Deploy path from latest approved
`origin/main`; dirty worktree deploys and direct Cloud Run service updates are
not valid normal STG deploy paths. Fast-forwarded this branch to the latest
`origin/main` commit and re-ran the consolidated verifier with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-after-origin-main-refresh`.
The helper exited `0` with all gates passing and `oci_db_check=reachable`.
Generated report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-after-origin-main-refresh/report.md`.
The report records `HEAD == origin/main`, `worktree_dirty=true`,
`stg_images_match_head_commit=false`, and `stg_proves_current_worktree=false`;
STG Cloud Run still points at the older base image, so live staging role-flow
E2E remains unproven until the feature is committed/approved and deployed by
the authorized STG path.

Update 2026-08-26 resume 33: widened the hygiene audit from feature docs to all
changed/new text files in the branch. The scan found that the touched
`tools/local/phone-qa-throwaway-seed.sh` still documented and enforced a
non-OCI database port. Updated that touched script to require the OCI DB
environment, an open OCI tunnel, `GOATOS_PHONE_QA_SEED_CONFIRM=oci-phone-qa`,
and `GOATOS_ENV=stg` or `oci`; preserved the `pc_care` grant addition. Re-ran
script syntax and whitespace checks. The only remaining broad-scan hits are
pre-existing admin-web runtime fallback URLs in code, not branch-added
docs/scripts guidance.

Update 2026-08-26 resume 34: moved the widened hygiene audit into
`tools/dev/vaccine-inventory-director-verify.sh` itself. The helper now builds a
changed-file target list across feature docs, scripts, XML artifacts, and
changed/new text files, then scans for personal path/name leakage and non-OCI
database/container guidance. The first run intentionally failed because the
helper scanned its own pattern text; fixed that self-scan issue by constructing
the loopback pattern without writing it literally. Re-ran the full helper with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-helper-wide-hygiene-fixed`.
The helper exited `0`, all gates passed, and `oci_db_check=reachable`. Generated
report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-helper-wide-hygiene-fixed/report.md`.
STG inspection remains unchanged: Cloud Run is still on the older image, so
`stg_proves_current_worktree=false`.

Update 2026-08-26 resume 35: re-ran `make api-client-check` after refreshing the
branch to latest `origin/main` and after widening the helper hygiene gate. API
client generation succeeded for app/admin/analytics. The target then exited at
the expected generated-client diff gate because this branch intentionally
updates the app OpenAPI contract and generated app client. Diff scope remains
limited to `contracts/openapi/app-api.yaml` and
`packages/api-client/src/generated/app-api.ts`; scan terms are the expected
`inventory_vaccine`, `task_proof`, `stock_fridge_video`,
`PCCareInventoryRequirement`, `PCCareTaskProof`, `task_proofs`,
`current_or_carry`, and `appRegisterPCCareTaskProof`.

Update 2026-08-26 resume 36: added the API client generation drift-scope check
directly to `tools/dev/vaccine-inventory-director-verify.sh`. The helper now
runs API client generation, requires generated-client drift to stay scoped to
`packages/api-client/src/generated/app-api.ts`, and verifies the expected
vaccine-inventory contract terms in the OpenAPI/generated-client diff. Re-ran
the full OCI-backed helper with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-api-client-gate`.
The helper exited `0`: backend focused suite, vaccination procurement support,
admin-web progress/typecheck, contract validation, API client drift scope,
migration duplicate guard, Android proof sync, Android inventory route/worklist
tests, feature docs/scripts hygiene, and OCI DB check all passed. Generated
report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-api-client-gate/report.md`.
STG inspection remains unchanged with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 37: refactored
`tools/dev/vaccine-inventory-director-verify.sh` so the API client drift-scope
gate and feature docs/scripts hygiene gate are named shell functions instead of
dense inline command strings. This keeps the helper easier to review while
preserving the same report labels and checks. Re-ran the full OCI-backed helper
with `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-helper-refactor`.
The helper exited `0`; all gates passed, including API client generation drift
scope, feature docs/scripts hygiene, and `oci_db_check=reachable`. Generated
report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-helper-refactor/report.md`.
STG inspection remains unchanged with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 38: scanned the 68 changed/new source files for common
debug or temporary markers. No actionable `TODO`, `FIXME`, `console.log`,
`println`, stack-trace, debugger, or dump markers were found; the only remaining
debug-pattern hit is the expected `fmt.Fprintln(os.Stderr, err)` startup error
path in `backend/cmd/kernel-worker/main.go`. Tightened the touched phone-QA seed
script's human-facing output and fixture description so it says OCI phone QA DB
and OCI fixture rather than non-OCI wording. Re-ran the script syntax and
targeted wording scans.

Update 2026-08-26 resume 39: re-ran the full consolidated verifier after the
source-marker and seed-script wording cleanup with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-source-marker-cleanup`.
The helper exited `0`; backend focused suite, vaccination procurement support,
admin-web progress/typecheck, contract validation, API client drift scope,
migration duplicate guard, Android proof sync, Android inventory route/worklist
tests, feature docs/scripts hygiene, and OCI DB check all passed. Generated
report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-source-marker-cleanup/report.md`.
STG inspection remains unchanged with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 40: added the source marker audit to
`tools/dev/vaccine-inventory-director-verify.sh` as a normal consolidated helper
gate. The first run caught the helper's own marker-pattern text, so the helper
now constructs those marker patterns without writing them literally. Re-ran the
full OCI-backed helper with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-source-marker-gate-fixed`.
The helper exited `0`; all prior gates passed, the new `source marker audit`
gate passed, and `oci_db_check=reachable`. The source-marker findings log
contains only the allowed kernel-worker startup stderr line. Generated report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-source-marker-gate-fixed/report.md`.
STG inspection remains unchanged with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 41: added `stg_deploy_contract_guard` to the
consolidated verifier. The guard checks `context/deploy-contract.json` for the
Slack/Cloud Build/Cloud Deploy authority, `origin/main` source ref, clean
worktree requirement, and break-glass-only deploy scripts, and checks the STG
runbooks for the same normal-deploy boundary. Re-ran the full OCI-backed helper
with `GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-deploy-contract-gate`.
The helper exited `0`; all prior gates passed, the new `stg deploy contract
guard` passed, and `oci_db_check=reachable`. Generated report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-deploy-contract-gate/report.md`.
STG inspection remains unchanged with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 42: removed stale test-comment wording that named the
old postgres integration harness directly and expanded
`feature_docs_and_scripts_hygiene` with a changed-source scan for stale
non-OCI database/container guidance. The first full run caught the verifier's
own allow-list regex, so the regex is now constructed dynamically like the
other hygiene patterns. Re-ran the full OCI-backed helper with
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-source-hygiene-self-hit-fixed`.
The helper exited `0`; all gates passed, including the expanded hygiene gate,
and `oci_db_check=reachable`. Generated report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-source-hygiene-self-hit-fixed/report.md`.
STG inspection remains unchanged with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 43: after adding the kernel-owned planner guard, re-ran
the full consolidated helper with the OCI tunnel open:
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-kernel-owned-full-oci`.
The helper exited `0`; backend, procurement, admin-web, contract, API client
drift, migration, Android, hygiene, source marker, STG deploy contract, final
report consistency, and OCI DB checks all passed. Generated report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-kernel-owned-full-oci/report.md`.
STG inspection remains unchanged with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 44: tightened `api_client_generation_drift_scope` so
the generated-client drift gate now requires the planner-guard wording
`human-plannable`, `Kernel-owned`, and `create wizard`. This keeps the OpenAPI
and generated app client contract from silently losing the rule that
`inventory_vaccine` remains readable on monitor/worklist surfaces but is not
offered by the human planner create wizard. Focused helper run:
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-api-drift-planner-guard`.
The helper exited `0`; all code/doc gates passed, with OCI skipped because the
tunnel was not open for this focused run. At that point, the latest full
OCI-backed passing run remained `codex-resume-20260826-kernel-owned-full-oci`.

Update 2026-08-26 resume 45: reopened the OCI tunnel and re-ran the full
consolidated helper after the API drift planner guard landed:
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-api-drift-full-oci`.
The helper exited `0`; every gate passed, including API client generation drift
scope with planner-guard terms and `oci_db_check=reachable`. Generated report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-api-drift-full-oci/report.md`.
STG inspection remains unchanged with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 46: rechecked generated-client drift after the latest
full OCI-backed verifier. The generated/API diff remains scoped to
`contracts/openapi/app-api.yaml` and
`packages/api-client/src/generated/app-api.ts`; there is no generated drift in
the admin or analytics OpenAPI/client files. The verifier latest-run guard
points to `codex-resume-20260826-api-drift-full-oci`.

Update 2026-08-26 resume 47: re-ran the broad text hygiene audit over the
current changed/untracked text-file target list. The scan covered `70` text
targets and found no personal identifiers, machine-specific paths, or non-OCI
database/container guidance. The only marker-audit findings were the expected
kernel-worker startup stderr line and historical report lines documenting that
same exception. Verified the loopback API defaults in
`apps/admin-web/lib/api/server.ts` are pre-existing `origin/main` behavior, not
feature-introduced guidance or script output.

Update 2026-08-26 resume 48: ran a focused consolidated helper pass after the
doc-hygiene checkpoint:
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-doc-hygiene-focused`.
The helper exited `0`; backend, vaccination procurement support, admin-web,
contract validation, generated-client drift scope, migration guard, Android,
hygiene, source marker, STG deploy contract, and final report consistency gates
all passed. OCI DB check was skipped because the tunnel was not open for this
focused run; the latest full OCI-backed passing run remains
`codex-resume-20260826-api-drift-full-oci`. STG inspection remains unchanged
with `stg_proves_current_worktree=false`.

Update 2026-08-26 resume 49: opened the OCI tunnel through the workspace helper
and re-ran the full consolidated verifier:
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-full-oci-after-doc-hygiene`.
The helper exited `0`; all backend, vaccination procurement, admin-web,
contract, generated-client drift, migration guard, Android, hygiene, source
marker, STG deploy contract, final report consistency, and OCI DB checks
passed. Generated report:
`.codex-goatos-render/vaccine-inventory-director/codex-resume-20260826-full-oci-after-doc-hygiene/report.md`.
The report records `oci_db_check=reachable`,
`stg_images_match_head_commit=false`, and
`stg_proves_current_worktree=false`; STG Cloud Run is still serving image tag
`a5a64b7bbff4`, not this dirty feature worktree.

Update 2026-08-26 resume 50: recomputed the current git worktree inventory from
`git status --short`. The branch still has `51` modified tracked files and now
shows `20` untracked status entries; the lower untracked number is because
evidence screenshots/XML are grouped under directory entries in porcelain
status. Updated the Current Worktree Inventory section to describe the current
status-line count instead of the older recursive evidence-file count.

Update 2026-08-26 resume 51: ran focused verification after the inventory-doc
correction:
`GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID=codex-resume-20260826-inventory-doc-focused`.
The helper exited `0`; all code/doc gates passed, including the final report
consistency guard. OCI DB check was skipped because the tunnel had been closed
after the prior full pass; the latest full OCI-backed passing run remains
`codex-resume-20260826-full-oci-after-doc-hygiene`.

Update 2026-08-26 resume 52: ran the requested branch-local OCI E2E instead of
treating live STG as the only proof target. Started the branch API locally on
isolated port `8090` against the OCI tunnel DB and verified `/readyz=204`.
Seeded a direct DB fixture `e2e-local-oci-20260826` for vaccination date
`2026-09-02`, task date `2026-08-26`, Coimbatore / Castro / `Parts 7-9`, 35
real alive goats, 10 ET+TT obligations, 25 PPR obligations, and one
`vaccination_drive_assignments` row. Ran the branch `cmd/kernel-worker` locally
against OCI; `pc-care-inventory-vaccine` logged `tasks_created=1` and the DB
readback proved task `e9a82501-5d6e-4dd5-9748-26b56c7dcc8b` assigned to
Chandrakant with `ET+TT=10` and `PPR=25`. Branch API checks proved director
visibility, operator exclusion, and CEO monitor visibility. Installed branch dev
APKs on both Poco devices with local bearer tokens and `adb reverse tcp:8080
tcp:8090`; captured director home/detail and operator home screenshots/XML in
`context/execution/vaccine-inventory-director-e2e-2026-08-26/local-oci-branch-resume/`.
The director detail screenshot shows ET+TT/PPR dose lines, fridge Photo/Video
proof controls, and disabled submit until proof. Fresh phone proof submission
was not completed in this pass because the attempted shutter tap opened the
Android notification shade; prior branch evidence and automated tests still
cover proof upload, submit, and verifier handoff.

Update 2026-08-26 resume 54: corrected the Android/backend navigation contract
per product feedback. The visible director stock entry is now under
Vaccination as nav key `vaccination_stock`, bottom-bar label `Stock`, href
`/vaccination/stock`, and is restricted to `pc_director` principals with
`pc_care.execute`; `/pc/inventory-vaccine` remains hosted only as a
compatibility route. Removed the weekday/date strip from the director Stock work
queue and from the actual vaccination drive list. Focused checks passed:
`go test ./internal/workforce/app` and
`./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.AppStartDestinationTest' --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.core.designsystem.icon.MeshaIconsNavKeyTest'`.
Physical Poco visual regression against the OCI-backed branch API captured
`context/execution/vaccine-inventory-director-e2e-2026-08-26/local-oci-ui-correction/director-stock-home-after-grants.png`
and `operator-vaccination-home.png`. Director XML shows `Vaccination` /
`Stock`, current/carry cards with due dates and `Open`/`Delayed` statuses, and
no `Preventive Care`, no `Vaccine Stock`, and no weekday labels. Operator XML
shows `Vaccination sheds` with `Drives` and `Alerts` only, no `Stock`, no
`Preventive Care`, no `Vaccine Stock`, and no weekday labels.
