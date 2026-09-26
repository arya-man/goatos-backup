# PR 445 Sales All Landing Progress - 2026-09-26

## Scope

Land PR #445, "Sales: end-to-end audit fixes (web, phone, backend) and nine maintainer decisions", to `main`.

## Current SHA

- Candidate before first landing gate: `dba03c2601aba7c5f8b8c4c72bd3ea20642bd499`
- `origin/main` before landing gate: `6343becb458d4205eab8f72ee2b25eae6fdda9f7`
- Rebased candidate after conflict resolution: `d7e1a6dbf6db9221499650a8137029e6a950ce35`

## Done

- Review pass completed with no remaining blocking findings after the Android Sales write-route gate fix.
- Prior blocking review finding about `sales_write=false` bypass via hosted routes was fixed by `SalesWriteAccess`, route-level unavailable screens, workflow tag-step gating, and focused tests.
- Focused review checks passed:
  - `git diff --check origin/main...HEAD`
  - `go test ./internal/workforce/app -run 'TestSalesWriteFlag|TestSalesTagFlag'`
  - `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew -q :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.ui.SalesWriteGateTest'`
- First `make land-main` attempt stopped safely during rebase on conflicts with current `origin/main`.
- Rebase conflicts resolved by preserving current `main` contracts while keeping PR behavior:
  - Sales farm filters kept dynamic tenant park validation while PR sorted buyer/farm-born rows server-side.
  - Sales service kept current UUID/farm validation plus PR close-date/date-window behavior.
  - Admin-web write marker kept exact read-only POST allowlist plus 4xx no-stamp behavior.
  - Sales drawer kept product-kind empty/dash semantics plus manure breed de-duplication.
  - Admin shell kept slim shell contract plus user-safe unavailable copy/telemetry.
  - Android bootstrap kept concurrent bootstrap flow plus primary role label.
- Focused post-rebase checks passed:
  - `git diff --check`
  - `go test ./internal/procurement/app ./internal/procurement/domain ./internal/sales/app ./internal/workforce/app`
  - `node --test --experimental-strip-types lib/api/write-marker.test.mjs lib/api/backend-write-marker-contract.test.mjs components/admin-shell-unavailable.test.mjs features/procurement/sales-format.test.mjs` from `apps/admin-web`
  - `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew -q :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.ui.SalesWriteGateTest' --tests 'sg.mesha.goatos.viewmodel.ProfileViewModelLogoutTest'`
- `features/procurement/buyer-table.test.mjs` could not run directly in this worktree because `typescript` was not present in `apps/admin-web/node_modules`; the full landing gate remains the required authority.
- First landing gate failed before push after 6s:
  - `offline-first-guard` flagged `SalesRepository.refreshLeadMeta`; resolved with an explicit guard annotation because the method persists the response through shared blob cache `putBlob`, not a feature DAO upsert.
  - `backend-foundations-guard` flagged gofmt drift in `loadwise_repository.go` and `workforce/domain/types.go`; resolved with `gofmt`.
- Failed landing steps rerun and passed:
  - `GOATOS_CI_ONLY_STEP='offline-first-guard' tools/ci/run-local-ci.sh android`
  - `GOATOS_CI_ONLY_STEP='backend-foundations-guard' tools/ci/run-local-ci.sh backend`

## Pending

- Run exact local landing receipt with `make land-main`.
- Confirm pushed `origin/main` equals the landed SHA.
- Confirm PR state after landing.

## Known Failures / Gaps

- No staging deploy or live smoke has been performed in this task.
- No Android device E2E was run in this task; review used code inspection and focused unit tests.

## Deployment State

- Not deployed.
- Not landed at the time this document was created.
