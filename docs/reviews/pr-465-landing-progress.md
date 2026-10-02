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
- Started the approved OCI tunnel on `127.0.0.1:15432`; focused diagnostic rerun
  `GOATOS_CI_ONLY_STEP='required PostgreSQL query plans' tools/ci/run-local-ci.sh
  query-plans` passed at `940b38336b4505fd5388199a8b5c08600fc95a2e`.
- Second `make land-main` attempt at `445cb7f019a1f6e437114dbd898b716c65008122`
  stopped before any push because the isolated admin-web dependency tree was incomplete:
  `eslint` and `tsc` were missing from `apps/admin-web/node_modules`.

## Pending

- Rebase the candidate onto current `origin/main`.
- Rerun the required local landing receipt with `make land-main`.
- Repair admin-web dependencies from the committed lockfile before the next receipt run.
- Verify local `HEAD`, `origin/main`, and remote `main` all match the certified SHA.

## Known Failures / Evidence Limits

- Docker is not running on this host, so the earlier review pass could not independently execute
  the DB-backed Postgres tests.
- The isolated worktree initially lacked admin-web dependencies, so `npm --prefix apps/admin-web
  run typecheck` could not start because `tsc` was missing.
- The focused query-plan rerun was diagnostic only and wrote no landing receipt; it does not
  authorize a push without the full `make land-main` gate.
- The admin-web dependency repair is infrastructure-only; the full landing receipt must be rerun
  after it.

## Current State

- Candidate with this progress note: `940b38336b4505fd5388199a8b5c08600fc95a2e`.
- Deployment state: not deployed; this request is only for main landing.
