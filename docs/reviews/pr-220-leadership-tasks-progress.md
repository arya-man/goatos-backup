# PR 220 Leadership Tasks Progress

## Goal

Finish PR 220 against Manju's ask: manual in-app tasks, assignable to director / park head / employee, notes plus voice notes plus video/file attachments, clear monitoring views (`Assigned to me`, `Assigned by me`, `Team progress`) on Android and admin web, with review loops using Manju-note and 1-month-commit/sync-architecture lenses.

## Current Cycle

- Synced to latest PR head `6fb1233f33d970718ea975fc3c3a924403965ea7`.
- Restored admin-web Tasks route, shell preview route, and design-system-aligned dashboard that the latest PR head had removed.
- Fixed review finding `LT-001`: seeded director cohorts now keep `configure`, which maps to `leadership_tasks.raise`, instead of being shadowed by `view+oversee`.
- Broadened the seed so every active app-backed workforce member with `person_access` receives `view+oversee` for mobile Tasks, making employee assignment explicit instead of role-grant-only.
- Added HTTP actor guard proving seeded director person access resolves to `can_raise=true`, while assignee access remains act-only.
- Added migration text guard proving the seed uses `LEFT JOIN` for role grants, grants `view+oversee` to receivers, and grants `configure` to director/CEO raisers.
- Split Team Progress monitoring away from raise authority: directors keep `Assigned by me` and the `+`, assignees keep `Assigned to me`, and only combined raise+act actors receive tenant-wide `Team progress` read visibility.
- Fixed second-cycle PR review finding `LT-001`: director seeds now include `do` so existing person-access rows do not shadow away `PenVisitsExecute`; CEO keeps `configure` without the pen-visit execution bundle.
- Fixed second-cycle PR review finding `LT-002`: production `/tasks` now requires the backend admin-web page contract and has a guard test; `/tasks-preview` remains the local screenshot/mock surface.
- Updated OpenAPI Leadership Tasks copy from the old director-to-CXO wording to Manju's manual two-way assignment model.
- Fixed third-cycle web completeness gap: production `/tasks` now fetches live `/app/leadership-tasks?scope=team_progress` data after the admin-web page-contract gate; fixtures are confined to `/tasks-preview`.
- Fixed third-cycle scope bug: `ScopeKeyOrDefault` now trims once and compares the trimmed key, so whitespace query values do not drop monitor users out of Team Progress.
- Cleaned stale Leadership Tasks comments/schema copy that still described one-way director-to-CXO tasks.
- Fixed fourth-cycle PR review finding `LT-220-01`: backend admin-ui now publishes the `leadership-tasks` `/tasks` page contract, primary nav leaf, route label, module page mapping, and web-capability surface so production admin web can render Tasks instead of redirecting away.
- Fixed fourth-cycle PR review finding `LT-220-02`: attachment download monitor coverage now uses `CanMonitor`, not accidental `CanRaise`, so the guard matches the intended Team Progress/read-monitor authority.
- Preserved the existing Approvals -> Verify primary-nav invariant and placed Tasks after Verify, with tests covering CEO/CXO receipt of the Tasks page contract and nav leaf.
- Fixed fifth-cycle PR review finding `LT-220-01`: migration now seeds `leadership_tasks` on `web` with `view` for CEO/CXO and director cohorts, and rollback tracking is surface-scoped so mobile raise/act rows remain separate from admin-web page narrowing.
- Fixed fifth-cycle PR review findings `LT-220-02` and `LT-220-03`: production admin-web activity is derived from live task fields, preview-only rows stay behind `/tasks-preview`, and scope chips navigate through `?scope=` so `/tasks` fetches the selected backend scope instead of always forcing Team Progress.
- Fixed fifth-cycle Manju-note finding `LT-WEB-001`: admin-web now fetches backend assignees and posts a real create form to `POST /app/leadership-tasks` when the backend page says `can_raise=true`.
- Fixed fifth-cycle Manju-note finding `LT-ANALYTICS-001`: Leadership Tasks list/open/raise/edit/status/attachment/audio/failure events are all in the durable backend analytics allowlist.
- Fixed sixth-cycle PR review finding `LT-220-01`: both migration upsert CTEs now return `surface`, and the migration guard asserts this so the web/mobile seed executes instead of failing at `recorded_rows`.
- Fixed sixth-cycle PR review finding `LT-220-02`: admin-web create uses a stable idempotency key rendered with the form and rejects invalid/missing keys instead of minting a fresh key inside the server action.
- Fixed sixth-cycle Manju/PR web attachment findings: admin-web create accepts existing uploaded proof refs with kind/name, posts them as non-empty attachment refs, and the monitor panel renders live task brief/comment plus task-scoped attachment links through an admin proxy that resolves backend signed URLs.
- Fixed seventh-cycle Manju-note judge findings: the admin-web attachment proxy now normalizes backend-relative signed URLs before redirecting, and the web shell maps the backend `clipboard-list` nav icon token so Tasks does not silently fall back to the generic icon.
- Fixed seventh-cycle PR review findings: web raisers are now intentionally seeded with `web:view+configure`, and admin-web create can upload real file/audio/video/photo attachments through the proof upload pipeline before raising the leadership task.
- Fixed eighth-cycle judge findings: admin-web Leadership Task uploads now always register proof rows as `attachment` while preserving MIME-derived task attachment kind, and proof upload creation sends the stable per-attachment `Idempotency-Key` header instead of metadata-only replay markers.
- Fixed ninth-cycle PR review findings: CEO/CXO web seed now includes `oversee` with `configure` so Team Progress monitor authority survives person-access shadowing, while director web rows remain raise-only; admin-web create now accepts multiple selected attachments.

## Acceptance Status

- COO representation: accepted as the existing CEO/CXO leadership desk role (`ceo_internal`) plus combined raise+act/no-pen-visit person access. There is no separate `RoleCOO` in the current RBAC vocabulary.
- Employee assignment: accepted as every active app-backed workforce member with `person_access` and mobile Leadership Tasks `view+oversee`.
- Notes: accepted for this PR as title/body plus one assignee note/comment field and attachments. Threaded Jira-style activity is not built in this PR.
- Seed/E2E data: migration is implemented and guarded locally. Real OCI/staging readback is not part of the committed test suite here.
- Screenshots: Android evidence remains unit/Paparazzi fixture based; web evidence includes Chrome `/tasks-preview` with shell/sidebar. Production `/tasks` is live-data backed behind the backend page contract.

## Validation Log

- Passed: `go test ./internal/leadershiptasks/... ./internal/permissions/... ./internal/workforce/app -run 'Leadership|Task'`.
- Passed: `go test ./internal/leadershiptasks/app ./internal/leadershiptasks/domain ./internal/leadershiptasks/adapters/http ./internal/permissions ./internal/workforce/app ./internal/adminui/app`.
- Passed: `go test ./migrations/postgres -run 'LeadershipTasksTwoWaySeed'`.
- Passed: `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk ./gradlew :app:testDevDebugUnitTest --tests '*LeadershipTask*'`.
- Passed: `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk ./gradlew :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.analytics.BackendAnalyticsAdapterTest'`.
- Passed: `npm run typecheck && npm exec -- eslint 'app/(admin)'/tasks/page.tsx app/tasks-preview/page.tsx features/leadership-tasks/leadership-tasks-page.tsx`.
- Passed: `npm run typecheck && npm exec -- eslint 'app/(admin)'/tasks/page.tsx app/tasks-preview/page.tsx features/leadership-tasks/actions.ts features/leadership-tasks/leadership-tasks-page.tsx lib/admin-route-page-contract.test.mjs lib/api/server.ts && node --test lib/admin-route-page-contract.test.mjs`.
- Passed: `npm run typecheck && npm exec -- eslint 'app/(admin)'/tasks/page.tsx app/tasks-preview/page.tsx 'app/api/leadership-tasks/attachments/[taskId]/[proofId]/route.ts' features/leadership-tasks/actions.ts features/leadership-tasks/leadership-tasks-page.tsx lib/admin-route-page-contract.test.mjs lib/api/server.ts && node --test lib/admin-route-page-contract.test.mjs`.
- Passed: `npm run typecheck && npm exec -- eslint 'app/(admin)'/tasks/page.tsx app/tasks-preview/page.tsx 'app/api/leadership-tasks/attachments/[taskId]/[proofId]/route.ts' features/leadership-tasks/actions.ts features/leadership-tasks/leadership-tasks-page.tsx lib/admin-route-page-contract.test.mjs lib/api/server.ts components/mesha-shell.tsx components/mesha-shell-nav-icons.test.mjs && node --test lib/admin-route-page-contract.test.mjs components/mesha-shell-nav-icons.test.mjs`.
- Blocked by pre-existing historical hot-table migration debt before applying `000281`: `bash backend/tests/integration/validate-postgres-migrations.sh`.
- Captured: Chrome shell screenshot at `apps/admin-web/.codex-leadership-tasks-web-sidebar.png`.
- Captured: Android Paparazzi images for CEO Team Progress, CEO assignee picker, Director detail, and Park Head detail under `apps/goatos-android/app/build/reports/paparazzi/devDebug/images/`.
- Passed: `node --test lib/admin-route-page-contract.test.mjs`.
- Second-cycle Manju-note judge found remaining gaps around static admin-web data, mobile-only capability declaration, COO naming, notes depth, and screenshots as local evidence.
- Second-cycle PR-review judge found `LT-001` director `do`/pen-visits shadowing and `LT-002` production admin route missing page-contract gate; both are patched and need re-judge.
- Third-cycle Manju-note judge found web static data, stale OpenAPI assignee copy, ambiguous progress-doc items; patched in current cycle.
- Third-cycle PR-review judge found web static data and whitespace scope normalization; patched in current cycle.
- Fourth-cycle Manju-note judge returned clean against commit `e68759993`, with only the documented v1 note-depth limitation.
- Fourth-cycle PR-review judge found missing backend admin-ui page/nav contract and an overly broad test actor; both are patched in current cycle and need re-judge.
- Fifth-cycle Manju-note judge found inert admin-web create and non-durable list/open/attachment/audio analytics; both are patched in current cycle and need re-judge.
- Fifth-cycle PR-review judge found missing web person-access seed, fake production activity, and cosmetic-only web scopes; all are patched in current cycle and need re-judge.
- Sixth-cycle Manju-note judge found web attachment-create and web evidence-inspection gaps; both are patched in current cycle and need re-judge.
- Sixth-cycle PR-review judge found migration CTE execution, web idempotency, and web attachment-create gaps; all are patched in current cycle and need re-judge.
- Seventh-cycle Manju-note judge found backend-relative attachment redirects and missing `clipboard-list` web-shell icon registration; both are patched in current cycle and need re-judge.
- Seventh-cycle PR-review judge found web/mobile surface authority ambiguity and proof-id-only web attachment creation; both are patched in current cycle and need re-judge.
- Eighth-cycle Manju/PR judges found media uploads registered with non-attachment proof types and metadata-only proof idempotency; both are patched in current cycle and need re-judge.
- Ninth-cycle PR judge found web Team Progress monitor authority shadowing and single-file web upload UI; both are patched in current cycle and need re-judge.
