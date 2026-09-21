# Dashboard Automation OCI Runbook

This runbook is the implementation companion for
`docs/engineering/dashboard-nightly-automation-plan.md`.

## Current status

The repository now contains the guarded automation entrypoint and validation checks. Enabling the OCI
timer still requires host-specific secrets and service wiring on the OCI VM.

Do not enable a timer until three manual dry or report-only runs produce clean receipts.

The installed OCI timers are:

- daily production smoke at `03:30 Asia/Kolkata` by default;
- post-main certification poll every 10 minutes by default. It fetches `origin/main`, runs once per
  new SHA, and records the last certified SHA under
  `~/.local/state/goatos/dashboard-automation/last-post-main-sha`.

Both timers use the same local env file and the same Slack notifier. They do not create OCI
resources.

The schedule and Slack destination are runtime configuration, not app code:

- `GOATOS_DASHBOARD_AUTOMATION_ON_CALENDAR` changes the daily smoke time.
- `GOATOS_DASHBOARD_POST_MAIN_ON_CALENDAR` changes how often OCI checks for a new `main` SHA.
- `GOATOS_DASHBOARD_SLACK_CHANNEL_ID` changes the alert channel.
- `GOATOS_DASHBOARD_SLACK_ALERTS=0` temporarily disables Slack delivery.
- `GOATOS_DASHBOARD_DATA_PARITY=0` and `GOATOS_DASHBOARD_API_LATENCY=0` are break-glass
  disables only; parity and latency are on by default.

Change these in `~/.config/goatos/dashboard-automation.env`, then rerun
`GOATOS_DASHBOARD_AUTOMATION_INSTALL=1 tools/dashboard-automation/install-oci-user-timer.sh` or
reload the user systemd timer. No Goat OS deploy is required. Do not put this schedule in the product
database: the automation must still be able to run when the product database or API is exactly what
is broken.

## Commands

Validate the repository-owned wiring before touching production or OCI runtime state:

```sh
make dashboard-automation-self-test
make dashboard-automation-guard
```

Run the production-safe smoke only after the authenticated read-only environment and explicit OCI
Always Free readback are present:

```sh
GOATOS_OCI_ALWAYS_FREE_CONFIRMED=1 \
GOATOS_API_BASE_URL=... \
GOATOS_TENANT_ID=... \
GOATOS_BEARER_TOKEN=... \
node tools/dashboard-automation/run.mjs --mode production-smoke
```

Install timers on the OCI runner only after the env file is ready:

```sh
GOATOS_DASHBOARD_AUTOMATION_INSTALL=1 \
tools/dashboard-automation/install-oci-user-timer.sh
```

The runner writes receipts under `.codex-goatos-render/dashboard-automation/<run>/receipt.json`.
Receipts redact bearer tokens, JWT-looking strings, and Anthropic key material.

## Slack alerts

The channel for human-facing automation alerts is `goatos-automation-alerts`
(`C0C39G90FCJ`). Enable delivery on OCI with either:

- `GOATOS_DASHBOARD_SLACK_WEBHOOK_URL`, or
- `SLACK_BOT_TOKEN` / `GOATOS_DASHBOARD_SLACK_BOT_TOKEN` plus
  `GOATOS_DASHBOARD_SLACK_CHANNEL_ID=C0C39G90FCJ`.

Slack delivery is on by default for the OCI runner when the webhook or bot token is present. The
notifier posts only on meaningful state changes: failure, missing auth/env blocker, self-healing PR
opened, or recovery after a prior red run. Repeated identical failures are deduped for the configured
cooldown (`240` minutes by default). Green runs stay quiet unless they recover a prior red run.

## Anthropic or agent review

The repository runner records the agent-review policy and budget caps. Self-healing PR creation is on
by default for failing OCI runs, but remains pull-request-only and must not auto-merge, auto-deploy,
approve itself, or write product/STG/OCI data. The actual OCI host adapter must enforce the same USD 1
per-run and USD 25 monthly caps before making model calls. Agent output is advisory only; it must not
turn a deterministic failure green.

## Free-tier guard

The preflight refuses to start a non-dry run when filesystem headroom is below 20 GB, when OCI
classification is unknown, or when OCI classification signals a non-free resource. The runner must not
create volumes, backups, public load balancers, paid resizes, or paid services.

## Android boundary

OCI dashboard automation covers Android-adjacent failures through API contracts, mobile WebView-sized
admin-web smoke, Firebase/distribution health checks when enabled, static Android guards in ordinary
CI, and server-side contract/fanout/latency checks. It does not run Android emulator, managed-device,
Macrobenchmark, or real-phone E2E on the OCI Always Free dashboard runner.

Keep emulator/device validation on the existing Android CI/local-device paths until the OCI host has a
measured RAM/CPU headroom receipt showing those jobs fit without starving PostgreSQL, Playwright, or
the dashboard backend. Without that receipt, promising emulator coverage on OCI would be false
coverage.

## Route and future-surface guard

`make dashboard-automation-guard` compares `apps/admin-web/app/(admin)/**/page.tsx` against the
deterministic smoke inventory. Adding a new admin route without smoke coverage fails the guard.

This is the future-proof part: the guard watches for new pages, not today's route count.

## Bug-fix pattern coverage since 2026-08-01

The dashboard automation scope is driven by the recurring bug fixes observed since 2026-08-01, not
only by the homepage or a hand-picked route list. The machine-readable coverage file is
`tools/dashboard-automation/bug-pattern-coverage.json`, and `make dashboard-automation-guard` fails
if it loses the required backend, admin-web, Android, regular-flow, latency, parity, or OCI
feasibility entries.

The scheduled OCI run must cover these regular read-only flows:

- full admin-web browser sweep in laptop and mobile/WebView-sized viewports;
- every registered admin page, nested tab, representative filter state, and safe detail/drawer
  overlay;
- critical business-data parity for goats, identifiers, locations/partitions, weighing, feed,
  procurement, sales, and vaccination;
- dashboard/API latency gates for normal read APIs, with bulk import/export/upload paths explicitly
  excluded from the sub-500 ms rule;
- failure-string and HTTP-error gates for `backend_down`, `Admin-web contract unavailable`,
  `The board could not be loaded`, and `Weights could not be loaded`.

The browser sweep is implemented in `apps/admin-web/scripts/smoke-visual-live.mjs`. It currently
guards every registered page route through `apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs`
and exercises roughly 130 route/tab/filter states in both laptop and mobile viewports. It also checks
screenshots, layout overflow/clipping, touch targets, overlapping controls, mobile table scrolling,
serious/critical accessibility violations, pagination controls, selected dialogs, selected drawers,
and known production failure strings.

Do not claim it clicks every possible drawer/modal on every page. Current coverage is full
route/tab/query-state coverage plus representative safe interaction coverage. Expanding a page's
meaningful drawer/modal coverage requires adding explicit interactions to `assertCoreInteractions`
or a dedicated `apps/admin-web/scripts/smoke-*-live.mjs` script, and the route guard must keep the
page itself in the visual sweep.

The same coverage matrix records recurring regression classes that must remain automated:

- PostgreSQL bind-arity/control-flow mistakes;
- route inventory drift and hidden contract failures;
- picker/query-param state loss across tab, park, scope, and sibling filter changes;
- mobile/WebView clipping, overflow, touch-target, and overlap failures;
- chart/KPI truthfulness and blank scaffolding;
- API fanout/latency regressions;
- STG-to-OCI data parity and field-reconciliation drift;
- weighing pen alias-vs-partition identity drift, including the Godel 2 ADG bug class;
- cross-client SOP/authored-form drift.

The weighing pen identity sentinel is `weighing_pen_alias_form_b_rows`. It fails when a weighing
campaign shed stores a pen as active shed + `partition_label` while a planner alias location row
exists for the same physical pen. That is the Godel 2 / Mandela / Castro failure shape that splits
ADG history, duplicates pens, freezes `gain_span_days`, or double-renders names such as
`Godel 2 - Part 1 - Part 1`. The check is read-only on both STG and OCI; repairing rows remains a
separate, human-approved database operation.

Manohar's 2026-09-21 Godel 2 note is tracked as a required pattern, not a one-off incident. The
guarded cases are:

1. A pen weighed under both key forms appears once in shed-weights, not twice.
2. `gain_span_days` follows the requested window and cannot stay fixed at seven days.
3. Different windows return independently computed ADG values, not the same frozen value.
4. `operational_location_display` never doubles the pen name.
5. Planner-to-create round-trip stores the same canonical pen identity the planner returned.
6. No campaign row may use active shed + `partition_label` when an alias location row exists.

Cases 1, 2, 3, 4, and 6 are read-only automation targets for STG/OCI parity and dashboard/API smoke.
Case 6 is already implemented by `weighing_pen_alias_form_b_rows`. Case 5 is a write-path round-trip,
so it must run only in a disposable preview/integration stack; it must never create or repair rows in
STG, prod, or the OCI parity database.

Android is included in the pattern review but not as a default OCI emulator/device job. The Android
project already needs a 4 GB Gradle heap, and emulator/macrobenchmark requires additional RAM, CPU,
SDK images, virtualization/KVM, and disk headroom. On the Always Free dashboard runner, Android
coverage is therefore limited to static/contract/unit/Paparazzi-capable checks when capacity is
proven; instrumented emulator/device, Bluetooth, camera, process-death, and Firebase distribution
flows belong in a separate Android lane or a host with explicit capacity proof.
