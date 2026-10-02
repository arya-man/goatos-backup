# PR 465 Landing Progress

## Scope

Land PR 465: move pen capacity from building-level Pens to partition-level Partitions, seed
partition capacities from the Sheds DB sheet, surface over-capacity partitions as warnings only,
and update the CEO AI shed capacity reporting view.

## Done

- Reviewed PR 465 source against the Goat OS backend, migration, reporting, OpenAPI, and admin-web
  lenses.
- Verified `git diff --check origin/main...HEAD` passed before the landing attempt.
- Verified `go test ./internal/configuration/domain` passed before the landing attempt.

## Pending

- Rebase the candidate onto current `origin/main`.
- Run the required local landing receipt with `make land-main`.
- Verify local `HEAD`, `origin/main`, and remote `main` all match the certified SHA.

## Known Failures / Evidence Limits

- Docker is not running on this host, so the earlier review pass could not independently execute
  the DB-backed Postgres tests.
- The isolated worktree initially lacked admin-web dependencies, so `npm --prefix apps/admin-web
  run typecheck` could not start because `tsc` was missing.

## Current State

- Candidate before this progress note: `5c4586658a8bb37fdd8a20b20fda38666c20cf65`.
- Deployment state: not deployed; this request is only for main landing.
