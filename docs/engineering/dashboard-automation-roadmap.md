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
  `goatos-dashboard-post-main.timer`) were **inactive** (`disabled`) and were **enabled on
  2026-09-23** on Ravi's authorisation, once PR #350 was on main. The daily production sweep is
  `OnCalendar=*-*-* 04:00:00 Asia/Kolkata` (RandomizedDelaySec=10m); post-main certification is
  `OnCalendar=*:0/10` (RandomizedDelaySec=2m). Both carry `Persistent=true`, so post-main fired
  immediately on enable to catch up its missed run. They are **user**-scope units in
  `/home/opc/.config/systemd/user/` — a system-scope `systemctl` check reports "No such file or
  directory" and reads as if they do not exist. `goatos-dashboard-automation-bootstrap.timer`
  remains disabled. `goatos-stg-readonly-proxy.service` is active.
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
- **Coverage ledger for lanes 2-5 built and independently verified.** Every commit on `origin/main`
  since 2026-08-01 is now classified exactly once, by the same method that produced lane 1's 919
  frontend assertions. Branch `auto/hist-20260923`,
  `tools/dashboard-automation/commit-classification/lane{2,3,4}.jsonl`, `lane5-android.jsonl`,
  `not-automatable.jsonl`, consolidated into `lane-checks.json` with
  `LANE-COVERAGE-REPORT.md`.

  | Bucket | Commits |
  |---|---:|
  | Lane 1 — already covered by PR #350's web ledger | 1023 |
  | Lane 2 — read-only data sanity SQL | 170 |
  | Lane 3 — read-only API contract + latency | 603 |
  | Lane 4 — write path on the OCI clone | 423 |
  | Lane 5 — Android | 1047 |
  | Parked, with a reason on every row | 785 |

  Verified independently: 3028 rows across the five new files, 3028 unique shas, zero duplicates,
  zero rows without a sha. 194 deduplicated checks (lane 2: 50, lane 3: 62, lane 4: 35, lane 5: 47),
  each carrying the full list of commits it covers.

  Build order follows the team's own repeat bugs, worst first: proof/media 564, verification gate
  356, published-version-not-locked 136, notification-not-delivered 110, offline-sync-queue 108,
  partition/pen-label 99, permission drift 97, idempotency/outbox 91, totals-don't-reconcile 74,
  double-count 54, latency 51.

  Parked breaks down as repo tooling/CI 363, docs only 276, Go-test-only 105, migration bookkeeping
  12, MCP connector plumbing 12, regenerated clients/gofmt 9, and 8 genuinely unroutable. Nothing
  parked is reported as covered.

  Five of lane 5's 47 checks need a **physical** device and say why a virtual Test Lab device would
  be a false green: proof capture, feed transport capture, weighing scan (RFID), roster scan
  (RFID/NFC), herd-signal tags (BLE).

  **Honesty flag carried in the data:** 495 of the 3028 routed commits are tied to their check by
  file path alone. The check is right to build, but the commit must be read before anyone claims it
  proves that exact behaviour.

- **The ledger drifts, so coverage is being made self-updating.** The classification above was taken
  at main `e4edc073f`; main is now `faa622283` and 58 commits landed within two hours, leaving ~52
  already unclassified. Counting by hand does not survive a moving main, so
  `tools/dashboard-automation/sync-coverage.mjs` — which already fails the guard when a new
  user-visible web commit has no assertion — is being extended to the four new lane ledgers. From
  then on a commit that no lane covers fails the guard instead of quietly eroding the
  "every commit since 2026-08-01" guarantee.
- **Coverage is now self-updating for lanes 2-5, and the count is pinned.** `sync-coverage.mjs` has
  a lanes-2-5 half beside lane 1's, running inside `make dashboard-automation-guard`. It fails on:
  an uncovered commit; a sha in two lane ledgers; a `checkId` with no check behind it; a row in the
  wrong lane file; parked work with no reason; a row naming a commit outside the window; a lane 2
  SQL that stops being a single read-only `SELECT` with a `LIMIT`; a lane 3 check that stops being
  `GET`; a lane 4 check that stops naming its tables; and a failure sentence that picks up SQL, a
  selector, a check code or a stack trace. `--write` parks new work as `needs-lane` — counted for
  the reconciliation, covered by no check. Tests: `lane-coverage.test.mjs` (23 cases) plus the
  existing 14 in `sync-coverage.test.mjs`, both wired into `make dashboard-automation-self-test`,
  and verified live by deleting a ledger row and watching the guard name the exact sha.

  Reconciled against main `faa622283`: lane 1 1023, lane 2 170, lane 3 609, lane 4 426, lane 5 1051,
  parked 829 — **4108 accounted for exactly once, none uncovered, 194 checks**, in 1.1s.

- **A defect in lane 1's coverage window, found while pinning ours.** The ledger behind PR #350's
  919 assertions uses `git log --since=2026-08-01` with no time. That is a git *approxidate*: git
  fills in the **current time of day**, so the window start slides forward as the day goes on. At
  03:46 IST the bare form returns 4104 commits and `--since=2026-08-01T00:00:00+05:30` returns 4108.
  The four it silently drops are user-visible work reported as covered when nothing covers it:

      1c71909f9  2026-08-01 01:58  feat: weighing leadership surface -- task list, task detail, create wizard
      9380f3a8f  2026-08-01 00:27  fix: unbreak leadership videos and put the planner on the planner surface
      5702801fe  2026-08-01 00:19  fix: resume the existing root entry on bottom-nav tab switch
      d07ab1e24  2026-08-01 00:04  feat: widen phone-QA fixture and share the weighing park chips

  Verified independently against `faa622283`. Lanes 2-5 pin their window in code as
  `COVERAGE_WINDOW`, with a test that the constant is a timestamp and not a bare date. **The lane 1
  side is PR #350's to fix** and has been reported to the session landing it; these files were left
  untouched.

- **Two further traps found and closed.** `readLedger()` globbed *every* `.jsonl` under
  `commit-classification/`, so the moment lanes 2-5 ledgers landed beside lane 1's, lane 1 would
  have silently believed it already covered every Android commit — now an explicit file list with a
  regression test. And the guard could never have gone green, because writing the ledger makes a
  commit that is itself for ever uncovered; bookkeeping-only commits are now auto-parked with a
  reason. The second was caught by the *pre-existing* end-to-end test in `sync-coverage.test.mjs`.

- **Path-only ties are reported, not hidden.** 496 of the routed commits are tied to their check by
  file path alone — lane 2 44, lane 3 193, lane 4 43, lane 5 216. The guard reports these per lane
  without failing, so the weaker links stay visible instead of being counted as proof.

- **Known red:** the full guard currently fails on lane 1's side — 7 admin-web commits from the
  350/363 CI work need `needs-assertion` entries in `feature-assertions.json`. That file belongs to
  PR #350 and was left alone.
- **PR #350 is on main.** It landed rebased, so its head sha is not an ancestor of `main`, but its
  commits are (`232cae667`, `b87508928`, `f560f6eac` and the rest) and lane 1's files are present on
  `origin/main`. Lane 1 is therefore **landed but not running**: both
  `goatos-dashboard-automation.timer` and `goatos-dashboard-post-main.timer` are still `disabled` on
  the OCI box. Nothing is being swept and the ~48 issues from the last production run are not being
  re-checked. Enabling them is the last item on lane 1's own "remaining" list.

- **Correction to the window defect above.** `sync-coverage.mjs` has no `--since` at all — it syncs
  `state.lastSyncedSha -> origin/main`. The four-commit hole is baked into the committed `.jsonl`
  data by whatever built the ledger offline, so there is no constant in that file to pin and the
  retroactive fix is a backfill, not a code change. The lanes 2-5 classifier does pin its own window
  (`COVERAGE_WINDOW`, tested as a timestamp rather than a bare date), and **all four commits are
  covered in `lane5-android.jsonl`** — they are missing only from lane 1's ledger. They are stored
  as short shas, which is why a full-sha grep reports them absent; sha length is being normalised
  across the ledgers so that trap does not catch the next reader.

- **Lane statuses after the first judge pass.** A three-way collision on `notify-slack.mjs` was
  caught before it landed: all three lanes had independently written their own `FINDING_KINDS`
  registry with incompatible signatures, which is a guaranteed `SyntaxError`, not a merge risk.
  Lane 4's registry survives as the single design — purely additive (+85/-0), `collectFindingKinds`
  running before `issueRules()`, and a `hasOwnIssues` guard; lanes 2, 3 and 5 take it verbatim and
  add one import and one registry entry each. Lane 4's golden-file test is the acceptance gate, and
  the judge regenerated that golden from a pristine tree rather than trusting the committed one.
  A lane-1-only receipt was proved to render **byte-identically** through all three lanes.

  A near-miss worth recording: lane 4's first registry spliced every lane's `issueRules()` into the
  shared matcher unconditionally, so a lane with **zero** findings silently relabelled a real lane-1
  issue. That reproduction is now a permanent self-test.

  Open and being fixed: lane 2's `LIMIT` guard passes when only a CTE is capped, so an uncapped
  query could reach the production-backed replica — being fixed structurally at the runner rather
  than with a smarter regex. Lane 3 has zero tests, so its redirect guard protecting the production
  bearer token is proved only by hand. Lane 4's `collectFindingKinds` has no try/catch, so one
  broken module silences **every** alert including lane 1's — the worst failure mode available,
  since the automation would go quiet exactly when something is wrong.

- **Lane 5 started**, branch `auto/lane5-20260923`, built from the miner's 47 specs. Free tier only
  (10 tests/day, 60 device-minutes/day on `goatos-stg`); no agent may incur billable device time.
  The five physical-device checks are never claimed as covered by a virtual run.

- **Lane 1 is now running.** Both timers enabled 2026-09-23 on Ravi's authorisation; a post-main
  certification cycle started immediately against main `faa622283`. The first alerts will be loud,
  because the ~48 issues from the last production run are still unfixed by deliberate choice —
  they are the sweep working, not a new regression. `notify-slack.mjs` mutes an identical signature
  for 240 minutes, so the 10-minute post-main cadence should not flood the channel.

### Enabling the timers: what it exposed

Enabling **both** timers was a mistake and `goatos-dashboard-post-main.timer` was disabled again
within minutes. Two real defects surfaced because of it, so the record is worth keeping.

- **`post-main-certification` is the wrong mode for this box.** It gates on the STG-to-OCI parity
  receipt, Lighthouse, Grafana and the Go vaccination lifecycle tests. The box has neither `gcloud`
  nor `go` installed, and no `GOATOS_DASHBOARD_PREVIEW_DATABASE_URL` or
  `GOATOS_ADMIN_WEB_BASE_URL`, so it failed 8 of 11 layers and posted "STG-to-OCI parity receipt
  invalid… Lighthouse… Grafana…" every 10 minutes. That is exactly the parity noise this document
  says made the old alerts useless. **Only `goatos-dashboard-automation.timer` (daily 04:00 IST,
  `production-smoke`) should run here** — its env sets none of the extras flags, so it runs the
  static guards plus the 146-route browser sweep and nothing else. Post-main needs `gcloud`, `go`
  and the two preview env vars on the box before it is switched on again.

- **Every live Slack alert was posting with no screenshots attached.** `postSlack`'s final bundle
  upload read `inline.length`; the parameter is `inlineShots`. In production the message posts,
  dedupe state is written, the per-issue replies upload, and then the bundle upload throws
  `ReferenceError: inline is not defined` — so the alert arrives without the HTML report and
  without the screenshots, which is the part anyone acts on. It shipped because
  `GOATOS_DASHBOARD_SLACK_DRY_RUN=1` returns before any upload, so `--self-test` and every dry run
  pass straight over that line. Fixed in **PR #367**: the caption is extracted into
  `evidenceComment()` so it is reachable from `--self-test`, both branches are asserted, and a
  guard reads the `postSlack` source and refuses any identifier not in scope there. Verified by
  reintroducing the bug — `--self-test` throws where it previously passed clean. The one-word fix is
  also applied directly to the box's checkout so tonight's sweep carries screenshots; that drift
  resolves on the next pull.

  **The general lesson, which applies to all four lanes:** `--self-test` passing says nothing about
  code that dry-run returns before reaching. The entire upload path — threaded replies, file
  uploads, captions, `thread_ts` — is untested by construction, and every lane's
  "renders byte-identically" proof is a proof about block JSON, not about anything that happens
  after `chat.postMessage`.

### Lane 2 is built, and its first live run found 17 real problems

Branch `auto/lane2-20260923`. **42 checks** — 30 from the list above, 12 folded in from the miner's
per-commit specs — run read-only against the STG-backed production replica. 25 green, **17 found
rows that should not exist (848 rows)**, 1 parked, nothing errored.

Worst first, as a person would read them:

- **139 upcoming vaccinations are planned for animals that have already been sold or died.**
  Operators are sent looking for animals that are not on the farm, and the round can never complete.
- **9 feeds are retired in the catalogue while current rates still name them** — the daily sheet
  keeps asking for feed the store no longer stocks.
- **4 feeds have been issued to the pens in greater quantity than was ever bought.** One item:
  1,076 kg issued, 0 kg purchased. Either purchases are missing or the sheets are issuing feed that
  never existed, so the stock figure cannot be trusted.
- **The sales screen says 701 animals sold on closed deals; the herd register says 155.**
- **500+ vaccination rounds are more than two days overdue** with animals still waiting and nothing
  recorded.
- **90 upcoming vaccinations name a pen the animal has since moved out of.**
- 67 counting exceptions unresolved over a week; 11 approvals with no person or time; 7 purchase
  loads whose head count does not match; 2,581 items of work waiting over 7 days; 5 deaths with no
  cause; 2 pens whose average does not equal total ÷ head count.

None of this is visible to lane 1 — a page can render perfectly while the number on it is wrong.

**A finding about the automation's own access, parked rather than softened:** the login this lane
uses, `goatos_app`, holds INSERT/UPDATE/DELETE/TRUNCATE on all 22 tables it reads and can CREATE in
`public`. It is **not** a read-only role. Every report records `roleIsReadOnly: false`. The checks
were still safe — session-level `PGOPTIONS`, `BEGIN READ ONLY`, one capped SELECT each, no statement
chaining — but the guarantee currently rests on the client, not on the grant. A genuinely read-only
role is the right fix.

Four checks were **built and then parked with the numbers proving they were noise**, which is the
standard for every lane: one fired on 26,066 of 27,394 rows because the rule did not model a ration
split across sessions; another was true of 160 of 160 exited animals by design, with nothing wrong on
any screen. A check that always fires teaches people to ignore the channel.

Slack output carries no SQL, table, column, check code or row value — `assertPlainEnglish()` throws
rather than render one.

**Two honest gaps** carried into the lane's doc: the end-to-end `production-smoke` cycle has not been
run on the box yet, so this layer is not yet trusted unattended; and lane 2's reply makes
`inlineShots` take its first branch, so on a run where lane 1 finds issues but none carry a
per-issue screenshot, the fallback screenshots are no longer attached. That line belongs to lane 4
and is being fixed.

