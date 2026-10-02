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
- First `make land-main` attempt at `279b1ccaa6ac752f9062916fda9b38589e0e483f`
  stopped before any push because the required query-plan PostgreSQL connection to
  `127.0.0.1:15432` was refused.

## Pending

- Rebase the candidate onto current `origin/main`.
- Restart or repair the approved OCI query-plan tunnel on `127.0.0.1:15432`.
- Rerun the required local landing receipt with `make land-main`.
- Verify local `HEAD`, `origin/main`, and remote `main` all match the certified SHA.

## Known Failures / Evidence Limits

- Docker is not running on this host, so the earlier review pass could not independently execute
  the DB-backed Postgres tests.
- The isolated worktree initially lacked admin-web dependencies, so `npm --prefix apps/admin-web
  run typecheck` could not start because `tsc` was missing.
- First landing attempt failed before push on infrastructure only: the query-plan database tunnel
  was not accepting connections.

## Current State

- Candidate with this progress note: `279b1ccaa6ac752f9062916fda9b38589e0e483f`.
- Deployment state: not deployed; this request is only for main landing.
