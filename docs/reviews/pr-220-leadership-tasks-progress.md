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

## Known Open Items To Re-judge

- Whether COO is represented by the existing combined raise+act/no-pen-visit leadership desk or needs a separate named role.
- Whether "any employee" requires additional product constraints beyond every active app-backed workforce member with `person_access`.
- Whether Jira-like notes must become threaded comments/history beyond the current task comment path.
- Whether OCI-backed E2E is required now; current screenshots are local/Paparazzi/Chrome fixture evidence.

## Validation Log

- Passed: `go test ./internal/leadershiptasks/... ./internal/permissions/... ./internal/workforce/app -run 'Leadership|Task'`.
- Passed: `go test ./migrations/postgres -run 'LeadershipTasksTwoWaySeed'`.
- Passed: `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk ./gradlew :app:testDevDebugUnitTest --tests '*LeadershipTask*'`.
- Passed: `npm run typecheck && npm exec -- eslint 'app/(admin)'/tasks/page.tsx app/tasks-preview/page.tsx features/leadership-tasks/leadership-tasks-page.tsx`.
- Captured: Chrome shell screenshot at `apps/admin-web/.codex-leadership-tasks-web-sidebar.png`.
- Passed: `node --test lib/admin-route-page-contract.test.mjs`.
- Second-cycle Manju-note judge found remaining gaps around static admin-web data, mobile-only capability declaration, COO naming, notes depth, and screenshots as local evidence.
- Second-cycle PR-review judge found `LT-001` director `do`/pen-visits shadowing and `LT-002` production admin route missing page-contract gate; both are patched and need re-judge.
