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
  `goatos-dashboard-post-main.timer`) are currently **inactive** (`disabled`), as is
  `goatos-dashboard-automation-bootstrap.timer`. `goatos-stg-readonly-proxy.service` is active.
- **Reaching the box:** `ssh goatos-oci`. The key is `~/.ssh/goatos_oci_dev_ed25519`; it was not
  in `~/.ssh/config`, so a bare `ssh opc@144.24.107.47` is refused with `publickey denied` and
  looks like missing access. A `goatos-oci` Host alias was added on 2026-09-23. Do not conclude
  access is missing without trying that key.
- On the box: `/home/opc/goatos-automation/goatos` is the checkout
  `goatos-dashboard-automation.service` runs from (via `tools/dashboard-automation/run-oci.sh`),
  `pr-350-dashboard-parity/` is a checkout of PR #350's head, and `reports/` holds past runs.
  `/home/opc/.config/goatos/dashboard-automation.env` (mode 0600) carries `GOATOS_BEARER_TOKEN`,
  `GOATOS_FIREBASE_REFRESH_TOKEN`, `GOATOS_FIREBASE_WEB_CONFIG` and `ANTHROPIC_API_KEY`. Load it
  by path the way `run-oci.sh` does; never print or copy a value into code, a receipt, a log or a
  commit.
- The box is normally **idle** (load average ~0.07) while the laptop is often pinned by
  `make land-main`. Heavy automation belongs on the box, not on the laptop.

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

**Table policy (decided 2026-09-23, Ravi):** lane 4 refreshes and restores **only the tables each
individual journey actually writes** — a per-journey snapshot -> write -> assert -> restore ->
prove-the-restore cycle. No wholesale nightly refresh of a big table list. Each journey declares its
own `writesTables`; the harness snapshots exactly those, and a journey that touches a table it did
not declare fails. This keeps the blast radius minimal and makes the restore provable per journey.
Analytics, telemetry and obligations tables are never touched.

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

## Build log

Progress on lanes 2-5 is appended here as it lands, so the PR is the record.

### 2026-09-23

- Environment facts above corrected: OCI SSH access **works** (`ssh goatos-oci`); the earlier
  "no key" reading was a missing `~/.ssh/config` entry, not missing access. Lane 4's live run is
  therefore not blocked.
- Lane 4 table policy decided and recorded above.
- Lanes 2, 3 and 4 under construction in parallel, each in its own isolated worktree off PR #350's
  head, with a standing judge reviewing continuously and a history miner classifying every
  non-frontend commit since 2026-08-01 into per-lane check specs (the same method that produced
  lane 1's 919 assertions; full reconciliation, never a sample).
- **Slack message contract:** each lane owns
  `tools/dashboard-automation/lib/finding-kinds/<lane>.mjs` and touches `notify-slack.mjs` with one
  import plus one `FINDING_KINDS` registry entry. `formatVisualIssuesMessage`, `formatSlackMessage`,
  `humanIssue` and `groupSlowPages` stay untouched, and every lane's self-test must prove a
  lane-1-only receipt still renders byte-identically. Slack text carries no SQL, table or column
  names, endpoint or field paths, status codes, selectors or check codes.
