# Pending issues handoff — 2026-07-12

Carry-over work log from the vaccination-closure + review-fix session. Everything **landed** is on
`main`; everything below is **open** and safe to pick up cold. Each item: ID · priority · where ·
plain-English · fix sketch · status.

## Already landed on main (context — do NOT redo)
- `c0fb00a0` FCM device deregister endpoint · `f01af2a8` FCM lifecycle ADR
- `6409d9db` completed the interrupted ScanRoster/execution keyset refactor + 4 real query-bug fixes
- `ebf33812` review-fix batch (P0 migration dup, park-scope, task_id optional, next_cursor,
  404 mapping, cursor validation, idempotent deregister, FEFO business-day, contract-first + client regen)

Prior defect ledger for the same window: `git show 310b8969:context/execution/last-35-commits-defect-ledger-2026-07-11.md`.

---

## A. Deferred from the review-fix batch (not blocking, but promised)

### A1 · P1-debt · vaccinationexecution compute-on-read (god-CTE) needs a read model
- Where: `backend/internal/vaccinationexecution/adapters/postgres/repository.go`; recorded in
  `tools/scale-guard/baseline.txt:15` (`god-cte ... # P1: live-recompute shed/exec reads; needs a read model`).
- Plain: the shed/execution screens recompute their numbers from raw event rows on every request
  instead of reading a pre-computed running tally. Fine at ~1k rows, slow at 1M.
- Fix sketch: build an incremental projection/read model (outbox-delta maintained), point the shed
  summary + execution reads at it, then delete the baseline line. Same pattern the processintegrity
  P0 entry (`baseline.txt:14`) also needs.
- Status: OPEN. Known/baselined debt (scale-guard passes because it's grandfathered). NOT a
  correctness bug; do not claim the file "scale-safe" until this lands.

### A2 · P3 · operations keyset orders by UUID, not operator-facing names
- Where: `backend/internal/vaccinationexecution/adapters/postgres/repository.go` (VaccinationOperations
  cohort_page ORDER BY `park_uuid, shed_uuid, stage`).
- Plain: park/shed lists are ordered by hidden ID codes, so the human-visible order looks random
  (all rows present, stable — just not alphabetical).
- Fix sketch: switch the keyset + ORDER BY to `(park_name, park_uuid, shed_name, shed_uuid, stage)`.
- Status: OPEN. Cosmetic; no data/perf risk.

### A3 · P1 · WIP mobile branch has a stale test fake
- Where: branch `agent/finish-vaccination-closure` (`310b8969`),
  `apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/viewmodel/VaccinationExecutionViewModelTest.kt:233`
  — fake still implements the OLD repo signatures, so the branch won't compile.
- Plain: a spare-parts box (side branch) has one part that no longer fits the new engine.
- Fix sketch: update the test fake to the current repo interface. Note the server-side half of the
  original mismatch (`nextCursor` vs `next_cursor`) is ALREADY fixed on main (`ebf33812`), so the
  app DTO `next_cursor` now matches the contract.
- Status: OPEN, branch-only (not on main).

---

## B. Mobile close-flow gaps (spawned as task chips)

### B1 · P1 · operator cannot reach the scan screen for multi-task sheds
- Where: `apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt` (OpenShedRecord
  routes to scanRoute only when the shed row has a non-null `taskId`/SOPTaskID);
  `feature/feature-record/.../RecordScreen.kt` is read-only (`RecordEvent.Close` only; `VaccineGroupRow`
  has no taskId/sopVersionId).
- Plain: every dev shed is multi-vaccine → shed-level taskId is null → tapping opens a read-only
  record drawer with no "pick a task → scan" button. So the operator can't execute those sheds.
- Confirmed on-device (emulator-5554) this session.
- Fix sketch: give the record drawer a per-task affordance; carry taskId/sopVersionId/taskRowVersion/
  batchId into the record read model + backend contract; add `RecordEvent.OpenTask` → `Routes.scanRoute(...)`.
- Status: OPEN. Task chip `task_77c5b27a`.

### B2 · P1 · mobile leadership verify/rework has no action
- Where: `apps/goatos-android/feature/feature-record/.../RecordScreen.kt` (read-only; `RecordEvent`
  only `Close`). Backend has `verifyTask`/`reworkTask` + `appVerifyVaccinationTask`/`appReworkVaccinationTask`.
- Plain: a director/park-manager can't accept or rework a submitted drive from the app — the
  close+verify half of the flow has no mobile button.
- Fix sketch: role-scoped verify/rework action in the record drawer wired to the app verify/rework
  endpoints; hide it for operators.
- Status: OPEN. Task chip `task_db8b1aa0`.

---

## C. Pre-existing main breakage (not introduced this session)

### C1 · P1 · tests/e2e references undefined `permissions.RoleManager`
- Where: `backend/tests/e2e/story_aa_orphan_singleton_shed_drive_test.go:196` and
  `story_aj_leadership_review_routes_test.go:102` use `permissions.RoleManager`, which is defined
  nowhere in `internal/permissions`. So `go vet ./tests/e2e` / `go test ./tests/e2e` is RED on main.
- Plain: two e2e tests reference a role constant that was never added, so the whole e2e test package
  won't compile. (Production `go build ./...` is fine — this is test-only.)
- Fix sketch: add the missing `RoleManager` constant to `internal/permissions` (if a manager role is
  intended) OR point the two tests at the correct existing constant. Then `go vet ./tests/e2e` compiles.
- Status: OPEN. Task chip `task_18bfba44`. Pre-existing — landed via a prior push, not mine.

---

## D. Parked multi-feature stash — NOT on main (stash@{0} + branch 310b8969)

`stash@{0}` ("temp-kernel-validation") is a ~200-file multi-session accumulation. Branch
`agent/finish-vaccination-closure` (`310b8969`) has my reconciliation of it (compiles: backend +
app). **5 unrelated modules fail their tests** — pre-existing breakage from other sessions, must be
finished/split before any of that body reaches main:

- **counts-source-import** — `cannot insert multiple commands into a prepared statement` (a stash
  import SQL has two statements in one prepared query).
- **processintegrity** — completed history leaks into the hot Action Center projection.
- **obligation/adapters/postgres** — test **HANGS** (10-min timeout; likely a non-terminating loop
  or a very slow query introduced by the stash).
- **vaccination/adapters/postgres + tests/e2e** — kernel capacity/eligibility stories fail.

Also parked on branch `310b8969` (needs to land WITH the offline-first mobile body, not alone):
- Mobile scan transport fix (`task_id` + `limit=20` + `next_cursor`) + logout clean-sweep
  (wipe caches/drafts/outbox/token). The backend already accepts these (task_id optional on main).

### D-FCM · mobile FCM SDK coupling (gated)
- Backend decouple endpoint is live (`c0fb00a0`) + ADR `docs/decisions/fcm-device-lifecycle.md`.
- OPEN mobile: add FirebaseMessaging SDK — fetch token on launch → send `push_token_hash` in
  registerDevice; `FirebaseMessagingService.onNewToken` re-register; call the deregister endpoint +
  `deleteToken()` on logout. Gated on the `goatos-prod` Firebase project; device-untestable until then.

---

## Fast-start next session
1. Read this file + `git log --oneline -6` (confirm main tip).
2. Pick by priority: C1 (unblocks e2e CI, small) → B1/B2 (mobile close flow) → A1 (read model, big) → D (stash triage).
3. For anything touching the parked stash, decide split vs finish FIRST — do not blind-pop `stash@{0}`.
