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
- Repaired `apps/admin-web/node_modules` with `npm --prefix apps/admin-web ci`.
- Focused diagnostic reruns passed at `95762d6e257044c98021d936d054e561ba11afe1`:
  `GOATOS_CI_ONLY_STEP='admin-web lint' tools/ci/run-local-ci.sh admin-web` and
  `GOATOS_CI_ONLY_STEP='admin-web typecheck' tools/ci/run-local-ci.sh admin-web`.
- Third `make land-main` attempt at `3a9e81a65e10daee329c2dc3febeff2d262c1d58`
  stopped before any push because `admin-web unit tests` could not launch Playwright Chromium
  (`chromium_headless_shell-1228` missing from the local Playwright cache). The same receipt passed
  required PostgreSQL query plans and command-board query plans.
- Installed the local Playwright Chromium cache with `npm --prefix apps/admin-web exec playwright
  install chromium` and recorded the repair at
  `7071951e13dd79311c4ea29f42e63ca77d8b912d`.
- Focused admin-web unit rerun at `7071951e13dd79311c4ea29f42e63ca77d8b912d` launched the
  browser tests, then stopped before any push because two browser fixtures exceeded hardcoded
  per-test timeouts on this local runner:
  `features/notifications/notification-bell-browser.test.mjs` and
  `scripts/procurement-answer-accessibility.test.mjs`.
- Applied a narrow test-only timeout adjustment for those browser fixtures so the local CI lane can
  complete after Playwright startup instead of terminating the test process early.
- Focused diagnostic rerun
  `GOATOS_CI_ONLY_STEP='admin-web unit tests' tools/ci/run-local-ci.sh admin-web` passed at
  `b8064d6060433ce5d51d9664473759e9d53c8936` with 1337 passing tests, 0 failures, and 3
  expected skips. The run was partial and wrote no landing receipt.
- Fourth `make land-main` attempt at `30a6ecfe6f69ea7103048a39ed9c9c150a927958`
  stopped before any push because `admin-web mock-fidelity` found stale procurement request-plan
  ignores and a duplicate `listLocations()` Promise fanout. The receipt had already passed required
  PostgreSQL query plans, command-board query plans, and the admin-web unit lane in that run.
- Repaired the procurement request-plan gate by naming the bounded parallel location taxonomy calls
  in `load-detail.tsx` and refreshing the bounded selected-detail debt marker in
  `source-entry-board.tsx`.
- Focused diagnostic rerun
  `GOATOS_CI_ONLY_STEP='admin-web mock-fidelity' tools/ci/run-local-ci.sh admin-web` passed after
  the procurement request-plan repair. The run was partial and wrote no landing receipt.

## Pending

- Rebase the candidate onto current `origin/main`.
- Rerun the required local landing receipt with `make land-main`.
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
- The focused admin-web lint/typecheck reruns were diagnostic only and wrote no landing receipt.
- The Playwright browser install is local test infrastructure only; rerun the full receipt after it.
- The focused admin-web unit rerun after browser install was diagnostic only and wrote no landing
  receipt.
- The focused admin-web unit rerun after timeout adjustment was diagnostic only and wrote no landing
  receipt.
- The fourth full landing attempt failed before any push and wrote no landing receipt.
- The focused admin-web mock-fidelity rerun after procurement repair was diagnostic only and wrote no
  landing receipt.

## Current State

- Last full landing candidate before procurement repair: `30a6ecfe6f69ea7103048a39ed9c9c150a927958`.
- Deployment state: not deployed; this request is only for main landing.
