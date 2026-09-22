# Dashboard automation: state and roadmap (2026-09-23)

The goal is one automation that finds what a person would notice on Goat OS
before the CEO does, and says so in Slack in plain English with a screenshot of
the exact spot. Lane 1 (production browser sweep) is built in PR #350. Lanes 2-5
are not built. This document is the handover.

## Environment facts (verified, do not re-derive)

- `dashboard.mesha.sg` and `api.goatos.mesha.sg` are **STG-backed production**:
  `goatos-stg:asia-south1:goatos-stg-core-db` (`docs/runbooks/deployment.md:293`).
  Everything the automation does against production is **read-only**.
- The **OCI box** (`opc@144.24.107.47`, `VM.Standard.A1.Flex`, 4 OCPU / 24 GB,
  `ap-mumbai-1`) holds a **writable clone** of that data in a container Postgres
  on `127.0.0.1:5432` (~44 GB). This is where write-path automation belongs.
  `tools/data/sync-stg-oci-analytics-parity.mjs` refreshes it from STG.
- **Parity is never a gate.** STG keeps moving (analytics, telemetry,
  obligations). Refresh only stable business tables; report parity as a note.
  Blocking the browser sweep on parity is what made the old alerts useless.
- OCI cost is **0.00 SGD** (Always Free, at the 4 OCPU / 24 GB limit). Check with
  `tools/dashboard-automation/oci-billing-report.sh`.
- Disk: 83 GB root, ~27 GB free (the 44 GB clone dominates), plus an unused
  15 GB `/var/oled`. **No `/dev/kvm`**, so an Android emulator cannot run there.
- Both OCI timers (`goatos-dashboard-automation.timer`,
  `goatos-dashboard-post-main.timer`) are currently **inactive**.

## Lane 1 - production browser sweep (BUILT, PR #350)

146 routes at laptop 1440 and Android WebView 390, ~25 min across 3 parallel
lanes. Per route: screenshot, layout and regression checks, read-only overlay
journeys, per-commit feature assertions. A failing page no longer stops the rest.

- Checks come from the team's own repeat bugs since 2026-08-01: chart labels
  crushed/clipped/tiny, text over text, mid-word wraps, content past its card,
  tables with no mobile scroll, sub-40px tap targets, raw codes / ISO dates.
  `apps/admin-web/scripts/lib/regression-checks.mjs`
- Overlays (drawers, dialogs, sheets) are opened read-only and proved on screen.
  `apps/admin-web/scripts/lib/overlay-journeys.mjs`
- **919 per-commit feature assertions** (608 runnable) cover every user-visible
  web commit since Aug 1. `tools/dashboard-automation/feature-assertions.json`,
  runner `apps/admin-web/scripts/lib/feature-assertions.mjs`.
- **Self-updating coverage**: `tools/dashboard-automation/sync-coverage.mjs`
  fails the guard when new commits are uncovered or an assertion's on-screen
  target disappears. Ledger: `tools/dashboard-automation/commit-classification/`
  (2041 commits). `--write` parks new work as `needs-assertion`.
- Slack: summary grouped by cause + slow pages with seconds and links, then one
  threaded reply per issue with its link and its own red-boxed screenshot, plus
  an HTML report. `tools/dashboard-automation/notify-slack.mjs`
- Android app commits (571 since Aug 1) are **not covered by this lane**.

Remaining for lane 1: land PR #350 via `make land-main`, re-enable both timers,
watch one cycle.

## Lane 2 - data sanity on production (NOT BUILT, ~1h)

Read-only SQL against the STG replica (`GOATOS_STG_READONLY_DATABASE_URL`, via
the running `goatos-stg-readonly-proxy` on 127.0.0.1:5455). Pure reads, no
writes, seconds to run, twice daily alongside lane 1.

Checks to implement (each returns offending rows, capped):
- herd total vs alive + sold + dead + culled; park/pen sums vs park totals
- an animal in two pens, or in a pen and sold/dead
- impossible values: non-positive weights, implausible weight jumps, feed issued
  greater than purchased, doses above animals in the pen
- dates: future-dated records, death before birth, capture after submission
- orphans: weighings/sales/loads pointing at missing parents
- stuck work: drives in progress > 2 days, verifications pending > 7 days
- cross-screen totals: sales sold count vs herd register sold rows

Report through the same Slack path as a new finding kind ("Data does not add
up"), with the rows in the HTML report.

## Lane 3 - backend API checks (NOT BUILT, ~1h)

Call the production API directly (read-only, bearer token already in the OCI env)
and assert: no 5xx, response shape matches the page contract, required fields
present, no `null`/`NaN`/empty enum leaking, latency within
`config.json: apiLatencyPolicy` (hot paths p95 < 500ms). Runs with lane 1.
Contracts live in `backend/internal/adminui/app/` and `contracts/`.

## Lane 4 - write-path on the OCI clone (NOT BUILT, ~2h)

The flows production cannot test. Point an API + admin-web instance at the OCI
Postgres (the box already runs API processes on 127.0.0.1:18873/18874 for
preview Playwright; see `runPreviewPlaywright` in
`tools/dashboard-automation/run.mjs`).

1. Refresh only stable business tables from STG (herd, pens, feed config, SOPs,
   sales, procurement). Never sync analytics/telemetry/obligations.
2. Run write journeys, asserting both the screen and the database row:
   publish a vaccination plan version; change a feed rate; publish an SOP;
   create and move a task; record a sale; approve a verification.
3. Assert the business rules that keep regressing (a feed item cannot be deleted
   while a ration names it; a published plan version stays locked; partition
   labels survive a move).
4. Restore the touched tables and prove the restore worked.

Nightly, and on demand before a release. Never point this lane at production.

## Lane 5 - Android on virtual devices (NOT BUILT, ~2-3h)

Firebase Test Lab **virtual devices** (`goatos-stg` project). Free tier: 10
tests/day, 60 device-minutes/day; Blaze $1/device-hour virtual, $5 physical.
The OCI box cannot host an emulator (no KVM); it can still build the APK if a
JDK + SDK are installed, but disk is tight.

Cover, in priority order:
- sync architecture: offline queue, retry, conflict, resume after force-stop
- upload failures: kill mid-upload, network loss, server 500, low storage
- the screens and copy of the 571 Android commits since Aug 1
Keep a handful of **physical**-device runs before releases for camera/video
capture, BLE and RFID, which a virtual device cannot exercise.

## Working agreements

- Findings must name the page, the device, what a person sees, and carry a
  screenshot with the problem outlined in red. No check codes or selectors in
  Slack.
- Anything that cannot be verified read-only is parked with a reason, never
  faked green.
- Coverage is derived from **every** commit, never a sample.
- Merging to `main` is `make land-main` with a green receipt. Never
  `gh pr merge`.
