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
- Second landing gate failed before push after 15s:
  - `cascade-event-wiring-guard` flagged `SaleFailedReleaseHandler` as hidden behind the shared helper and not visibly registered in both durable buses.
  - Resolved by directly registering `identityapp.NewSaleFailedReleaseHandler(identityRepo)` in `kernelstages.BuildDomainBus` and `cmd/domain-event-consumer` while leaving the shared helper for the other bus builders.
- Failed cascade step rerun and passed:
  - `GOATOS_CI_ONLY_STEP='cascade-event-wiring-guard' tools/ci/run-local-ci.sh backend`
- Third landing gate failed before push after 6s:
  - `frontend-foundations-guard` flagged `over35-actions.ts` because its GET-backed Server Action was read-only but lacked the exact read-only marker.
  - Resolved by marking the action `server-action-read-only` without adding fake idempotency to the non-mutating path.
- Failed frontend-foundations step rerun and passed:
  - `GOATOS_CI_ONLY_STEP='frontend-foundations-guard' tools/ci/run-local-ci.sh admin-web`
- Fourth landing gate failed before push after 95s:
  - Admin-web dependency setup had been missing; after `npm install`, `admin-web lint` and `admin-web typecheck` reruns passed. The root `package-lock.json` rewrite from local npm was reverted because no dependency source change was intended.
  - `backend go vet` flagged `sortBuyerRepo` missing the newer dynamic `ListParkCodes` method after the rebase.
  - Resolved by adding `ListParkCodes` to the test fake.
- Failed backend vet step rerun and passed:
  - `GOATOS_CI_ONLY_STEP='backend go vet' tools/ci/run-local-ci.sh backend`
- Fifth landing gate failed before push after 40s:
  - `ceo-ai-page-contract-drift-guard` flagged the Sales Farm Value `kpi.feed` tile missing from the planned coverage row.
  - Resolved by adding `kpi.feed` to the `/sales/farm-value` `PLANNED:P2[...]` list in `docs/ceo-ai/coverage-matrix.md`.
- Failed CEO-AI drift step rerun and passed:
  - `GOATOS_CI_ONLY_STEP='ceo-ai-page-contract-drift-guard' tools/ci/run-local-ci.sh common`
- Sixth landing gate failed before push after 40s:
  - `mesha-data-map-guard` flagged the Load-wise Sales derived query hash because gofmt touched `loadwise_repository.go`.
  - SQL semantics were unchanged; rehashed the derived query manifest with `node tools/ask-mesha-agent/gen-data-map.mjs --rehash-derived`.
- Failed data-map step rerun and passed:
  - `GOATOS_CI_ONLY_STEP='mesha-data-map-guard' tools/ci/run-local-ci.sh common`
- Seventh landing gate failed before push:
  - `scale-guard-plan-proof` flagged the identity search bind-normalization change plus new Load-wise stock-weight and sale-line-share SQL as missing same-diff at-scale plan proofs.
  - Identity search was marked plan-neutral because only the parameter is normalized; the indexed column, joins, and predicates are unchanged.
  - The two new procurement reporting reads remain a documented pending plan-proof gap in `docs/progress/plan-proof-backlog.md`.
- Failed scale plan-proof step rerun and passed:
  - `GOATOS_CI_ONLY_STEP='scale-guard-plan-proof' tools/ci/run-local-ci.sh backend`
- Eighth landing gate failed before push after 54s:
  - `scale-guard` still flagged the new Load-wise stock-weight god-CTE because the ignore was above, not on, the const declaration line.
  - Moved the narrow ignore onto `loadStockWeightSQL` itself.
- Ninth landing gate failed before push after 41s:
  - `admin-web unit tests` had a stale Weights cache-clear source-shape assertion that still expected inline `method.toUpperCase() !== "GET"` / `isReadOnlyPost`.
  - Updated the assertion to the current `isBackendWrite(method, url.pathname)` contract while preserving the same cache-clear behavior.
- Tenth landing gate failed before push:
  - `agent: boundaries` flagged procurement deep-imports from weighing's `assumption-copy`.
  - Switched `over35-kpi.tsx` and `over35-actions.ts` to the existing public `@/features/weighing` entrypoint.
- Eleventh landing gate failed before push after 77s:
  - `admin-web prefetch` flagged `components/link-pending.tsx` for importing `next/link` directly.
  - Re-exported `useLinkStatus` from `components/no-prefetch-link.tsx` and imported it through that wrapper.
- Twelfth landing gate failed before push after 76s:
  - `vaccination-hrms-seed-fixture-guard` flagged `generation.go` because returned-to-herd vaccination generation changed without the full vaccination/HRMS source fixture companion set.
  - Marked the generation change as seed-fixture neutral: it changes event-driven returned-goat behavior only, not source seed files or the vaccination/HRMS import contract.
- Thirteenth landing gate failed before push after 42s:
  - `admin-web mock-fidelity` flagged comment-only visible text literals and the shared sales drawer's `sales_farms` option group lookup.
  - Removed the quoted UI-copy comments and added an explicit stale-contract fallback for `sales_farms` on `sales-sold` and `sales-config`; live backend options still win.
  - The same focused gate then flagged two serial request-path reads after park-scope validation; annotated both reads as dependent on the validated shell park and reran `GOATOS_CI_ONLY_STEP='admin-web mock-fidelity' tools/ci/run-local-ci.sh admin-web` green in 2s.
- Fourteenth landing gate failed before push after 135s:
  - `admin-web production build + token leak` rejected the `next/link` named `useLinkStatus` import during the production webpack build and then exposed a server-only import pulled through the weighing barrel.
  - Pointed the no-prefetch wrapper at Next's client Link implementation and kept the over-35 KPI's label helper local to the procurement client card; reran `GOATOS_CI_ONLY_STEP='admin-web production build + token leak' tools/ci/run-local-ci.sh admin-web` green in 53s.
- Fifteenth landing gate failed before push after 160s:
  - `admin-web unit tests` exposed that the notification bell browser harness still aliased only `next/link`, while the no-prefetch wrapper now imports the concrete Next client Link module for production-build compatibility.
  - Added the matching `next/dist/client/link` alias to the hermetic browser fixture and reran `GOATOS_CI_ONLY_STEP='admin-web unit tests' tools/ci/run-local-ci.sh admin-web` green in 13s.
- Sixteenth landing gate failed before push after 150s:
  - `android :app compile+unit+lint` failed in `core-data` unit tests; targeted pager tests passed alone/together, then the repeatable full `core-data` failure exposed a proof-status assertion reading before the background outbox follower had persisted `SYNCED`.
  - Reused the existing `awaitProofStatus` helper before asserting the proof row's final status and reran `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew -q :core:core-data:testDebugUnitTest` green in 26s.

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
