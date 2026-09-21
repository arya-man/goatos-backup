# Leadership Task Party Push Fix - 2026-09-21

## Scope

Stop Leadership Task push notifications from fanning out to role audiences. Raised-task and status-change pushes must reach only the task parties: the assignee, the raiser, or both when a third party changes status.

## Done

- Removed audience resolver use from Leadership Task raised/status push handling.
- Added a regression test that fails if the leadership audience resolver is used for task party pushes.
- Confirmed the staging symptom: task #16 was raised by Manju, assigned/changed by Dinakar, and Hemanth received it through the feed director audience fan-out.

## Pending

- Run final `make land-main` from this isolated worktree.
- Verify local and remote `origin/main` match the landed SHA.

## Tests

- `go test ./internal/notificationbridge` from `backend`: passed.

## Judge Status

- Focused regression coverage added.
- Full landing receipt pending.

## Current SHA

- Base: `b4e48f841`
- Candidate commit: `7a541376c`

## Deployment State

- Not deployed.
- Main push pending local landing receipt.
