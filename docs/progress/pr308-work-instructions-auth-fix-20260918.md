# PR 308 Work Instructions Authorization Fix - 2026-09-18

## Scope

- Fix the PR 308 review finding where `work_instructions.execute` could reach birth/death workflow list, detail, answer, and complete routes.
- Preserve Work instructions access to general SOP workflow list/detail/write routes.

## Done

- Added a handler-level capability guard:
  - Non-general `/app/workflows?module=...` lists require `counts.write`.
  - Colostrum lens detail requires `counts.write`.
  - Workflow detail and write routes allow `work_instructions.execute` only when the loaded workflow module is `general`.
- Added regression tests for:
  - Work-instructions users cannot list birth workflows.
  - Work-instructions users cannot read or mutate death workflow detail/actions.
  - Work-instructions users can read and answer general workflow runs.

## Pending

- Commit and push to PR branch `feat/sop-studio`.

## Tests / E2E

- `go test ./internal/tasks/adapters/http` from `backend` - PASS.
- `go test ./internal/tasks/...` from `backend` - PASS.
- `git diff --check` - PASS.

## Known Failures

- None yet.

## Current SHA

- Before fix: `84020361847a17b828f1abcefbff38b07e2d179e`

## Deployment State

- No deploy requested or performed.
