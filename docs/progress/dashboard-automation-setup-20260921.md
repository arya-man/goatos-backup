# Dashboard Automation Setup Progress — 2026-09-21

## Scope

Implement the repo-side dashboard automation suite for PR #331: module-wise
read-only Playwright journeys, guard coverage that moves with admin-web
features, backend/data parity hooks, performance/observability hooks,
Slack/self-healing hooks, OCI post-main/daily runtime wrappers, receipts, and
local proof gates.

## Done

- Added `tools/dashboard-automation/config.json` with desktop/mobile viewports,
  required production failure strings, free-tier headroom policy, and agent
  budget caps.
- Added `tools/dashboard-automation/check-static-inventory.mjs`.
  - Derives admin routes from `apps/admin-web/app/(admin)/**/page.tsx`.
  - Derives smoke coverage from `apps/admin-web/scripts/smoke-visual-live.mjs`.
  - Fails when a route is added/removed without smoke coverage or explicit
    exclusion.
  - Fails when required bad strings are no longer asserted.
- Added `tools/dashboard-automation/run.mjs`.
  - Writes redacted receipts under `.codex-goatos-render/dashboard-automation/`.
  - Refuses to run OCI work unless `GOATOS_OCI_ALWAYS_FREE_CONFIRMED=1`.
  - Refuses paid-capacity opt-in.
  - Stops production smoke as `auth_blocked` when auth env is missing.
  - Keeps the PostgreSQL preview phase fail-closed until a disposable preview
    database URL is provided.
- Added `tools/dashboard-automation/agent-review.mjs`.
  - Produces structured advisory review metadata.
  - Does not require or print API keys.
  - Does not override deterministic failures.
- Added `tools/dashboard-automation/run-oci.sh`.
  - Sources runtime secrets from an env file outside Git.
  - Refuses dirty tracked checkouts and SHA mismatch with `origin/main`.
  - Writes each run under a SHA/mode-stamped artifact directory.
- Added `tools/dashboard-automation/install-oci-user-timer.sh`.
  - Creates user-systemd service/timer files only on explicit
    `GOATOS_DASHBOARD_AUTOMATION_INSTALL=1`.
  - Defaults to dry-run and does not create OCI resources.
  - Uses `Nice`/I/O scheduling to reduce runner load.
- Added `tools/ci/check-dashboard-automation.mjs` and wired it into:
  - `make dashboard-automation-guard`
  - `make dashboard-automation-self-test`
  - `make dashboard-automation-production-smoke`
  - `make dashboard-automation-post-main-certification`
  - `make guardrails`
  - `tools/ci/run-local-ci.sh`
  - `tools/ci/guardrail-manifest.json`
- Updated `.agents/skills/goatos-code-review` so reviews apply this dashboard
  smoke/route/known-error-string lens across all admin-web changes, not only
  weighing.
- Tightened `tools/dashboard-automation/check-business-data-parity.mjs` after
  review:
  - STG and OCI parity now accepts only
    `GOATOS_STG_READONLY_DATABASE_URL` and
    `GOATOS_OCI_READONLY_DATABASE_URL`; it no longer falls back to a generic
    OCI database URL.
  - Every parity SQL statement is wrapped in `begin read only; ... rollback`.
  - `goat_sale_allocations` is now an explicit critical sales parity table,
    alongside the legacy `sales_sold_animal_tags` table.
- Tightened runner and artifact safety:
  - fatal runner errors are persisted into the receipt and force a failing
    status instead of disappearing into process control flow;
  - daily production smoke refuses non-production API origins;
  - agent-review evidence redacts route and viewport strings as well as file
    paths before writing review inputs.
- Added module-wise read-only Playwright journey coverage:
  - `tools/dashboard-automation/module-journeys.json` owns every live smoke
    route by business module: Weighing, Vaccination, Feed, Sales, Procurement,
    Counts/Herd, Health, Work Board/Action Center, Calendar, People, and
    Operations.
  - `tools/dashboard-automation/run-module-journeys.mjs` runs the existing live
    visual smoke per module with `GOATOS_SMOKE_ONLY_ROUTES` and
    `GOATOS_SMOKE_READ_ONLY=1`, producing a module receipt.
  - `tools/dashboard-automation/check-module-journeys.mjs` and the dashboard
    guard now fail if a dashboard route or smoke state is not owned by a module
    journey, so new features pushed to main must update automation coverage.
  - `tools/dashboard-automation/run.mjs` now uses module journeys for both
    production smoke and post-main certification, instead of one broad generic
    Playwright pass.
- Wired module journey manifests into the Playwright smoke itself:
  - `run-module-journeys.mjs` passes each module's `assertText` through
    `GOATOS_SMOKE_MODULE_ASSERT_TEXT` and each module's `safeClicks` through
    `GOATOS_SMOKE_MODULE_SAFE_CLICKS`.
  - `apps/admin-web/scripts/smoke-visual-live.mjs` observes required text from
    real rendered page bodies and fails the run if any module assertion is not
    seen.
  - The same smoke script performs manifest-declared read-only safe clicks,
    refuses unsafe names such as submit/save/approve/delete, closes overlays,
    and refuses safe-click execution when read-only smoke is disabled.
  - `check-module-journeys.mjs` now fails if any module journey lacks route
    ownership, real coverage dimensions, visible-text assertions, or at least
    one read-only safe-click target.

## Pending before enabling a real timer

- Actual OCI timer installation on the VM after the env file exists.
- Exact disposable PostgreSQL clone/create/drop commands for the OCI source.
- Preview backend/admin-web lifecycle on private loopback ports.
- Production authentication injection path that does not store credentials in Git.
- Real API-funded agent call implementation after budget telemetry is confirmed.
- First three report-only lifecycle runs with receipts.

## Validation

- `node tools/dashboard-automation/check-static-inventory.mjs --self-test`
- `node tools/dashboard-automation/check-static-inventory.mjs`
- `node tools/dashboard-automation/agent-review.mjs --self-test`
- `node tools/dashboard-automation/run.mjs --self-test`
- `node tools/dashboard-automation/check-module-journeys.mjs --self-test`
- `node tools/dashboard-automation/check-module-journeys.mjs`
- `node tools/dashboard-automation/run-module-journeys.mjs --self-test`
- `bash -n tools/dashboard-automation/run-oci.sh`
- `bash -n tools/dashboard-automation/install-oci-user-timer.sh`
- `make dashboard-automation-self-test`
- `make dashboard-automation-guard`
- `node tools/ci/check-dashboard-automation.mjs --self-test`
- `node tools/ci/check-dashboard-automation.mjs`
- `git diff --check`
- `node --test --experimental-strip-types apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs`
- `node --test --experimental-strip-types apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs tools/perf/api-latency-policy.test.mjs`
- `make guardrail-registration-guard`
- Read-only safety scan of `tools/dashboard-automation`,
  `tools/ci/check-dashboard-automation.mjs`, and the dashboard automation docs:
  no write SQL/restore/dump/sync path in the new parity runner; matches were
  limited to forbidden-action deny-list entries and key-presence detection.

Note: `npm --prefix apps/admin-web run test -- scripts/smoke-visual-route-coverage.test.mjs`
was not a focused run; the npm script prepended the full admin-web test globs and failed on existing
`typescript` package resolution for unrelated tests in this fresh worktree. The direct node test above
is the focused smoke route coverage proof and passed.

Live STG/OCI parity readback did not run in this shell because both
`GOATOS_STG_READONLY_DATABASE_URL` and `GOATOS_OCI_READONLY_DATABASE_URL` were
missing. The script remains fail-closed in that state and does not accept
generic write-capable env names.

## Agent review status

Automated judge/review hooks are wired and self-tested:

- `node tools/dashboard-automation/agent-review.mjs --self-test`: PASS.
- `make dashboard-automation-self-test`: PASS and includes the agent-review
  hook, self-heal PR hook, Slack notify hook, parity self-test, module journey
  guard self-test, module journey runner self-test, and shell syntax checks for
  the OCI/post-main/timer scripts.
- A focused final manifest judge was requested after commit `939572d66` to
  verify that `assertText` and `safeClicks` are consumed by Playwright. Before
  final push, the local implementation was additionally hardened so the module
  journey guard itself rejects missing safe-click contracts.
- `node tools/dashboard-automation/run.mjs --mode production-smoke --out-dir .codex-goatos-render/dashboard-automation/failclosed-check-2`
  - Expected failure: stopped after `oci-free-preflight` because local
    filesystem headroom was 11.0 GB, below the 20 GB threshold.
  - Verified no runtime production Playwright layer ran after the free-tier
    preflight failed.

## Current state

PR #331 head `3f645e19d75d8ba39cd3852396f4859660e507be` contains the
repo-side automation suite and local proof above. It does not create OCI
resources, does not install the timer without explicit
`GOATOS_DASHBOARD_AUTOMATION_INSTALL=1`, does not mutate production or staging,
and does not expose secrets. Runtime execution remains fail-closed until the OCI
free-tier, auth, and read-only database inputs are present on the runner.
