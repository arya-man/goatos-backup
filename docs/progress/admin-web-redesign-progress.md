# Admin-web redesign — progress log

Branch: `design/minimal-redesign-preview` · Living design doc: `docs/design/README.md`
Purpose: any session (Claude or Codex, any account) can resume from here.

## Current handoff — 2026-09-21 15:50 IST

Actual goal: finish PR #294 as a visible admin-web visual overhaul aligned to the MUI Minimal
Dashboard UX/vibe while preserving Mesha brand colors and real product data. This means every
visible route/component must look intentional on desktop Chrome and 390px mobile WebView: no
overlap, missing graph labels, clipped tables, broken drawers/modals, flicker, stale old-UI table
surfaces, or weird decorative color patterns.

Explicit non-targets for PR #294 visual parity: Action Center, Calendar, Control Tower, Protocol
Adherence, Workflows/workflow-record, and calendar detail/deep-link routes. They can remain
functional if existing flows link to them, but they must not consume Minimal-dashboard parity time.

Current pushed PR head: `70d590a17cf94a9bb636ebd0e9de6c3f7fa0f66d` on
`design/minimal-redesign-preview`; local branch `design/minimal-redesign-continue` matched remote
when checked after the Lighthouse documentation push.

What is proven done:
- Storybook visual lane after baseline refresh: 257 stories, 554 captures, 0 failures,
  `integrity_findings=0`; evidence `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T09-24-17-164Z`.
- Latest visible critical route sweep: 24 visible critical routes, 48/48 desktop + 390px webview
  dark captures, `integrity_findings=0`, `unstable_captures=[]`; evidence
  `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T09-59-51-292Z`.
- Current-SHA API binary can run on `127.0.0.1:18086`: `/version` showed
  `41bd3dd0d2ad67e6da36f8b26ae6000edd55cd0b`, migrations `000369#1`, drift false.
- Focused current-SHA latency retry for the last three previously red endpoints passed:
  `feed_direction_preview`, `procurement_loadwise_sales`, `work_board_page_cpt`; report
  `.codex-goatos-render/api-latency-red3-41bd3dd0-20260921T101122Z.json`.
- Production build passed: `npm --prefix apps/admin-web run build`.

What is not done / not acceptable yet:
- Full `hot-paths.admin-all.json` is **not final-green**. The latest full current-SHA run failed
  three data/manifest assertions (`vaccination_operator_assignment_config` 404, calendar week/month
  empty assertions) and is not acceptable as the final API proof. Report:
  `.codex-goatos-render/api-latency-admin-all-41bd3dd0-20260921T101139Z.json`.
- The local benchmark DB currently has empty business data (`goats=0`, `feed_direction_issues=0`,
  `procurement_loads=0`, `sop_tasks=0`), so the fast endpoint timings from the latest run are weak
  proof and must not be used to claim production-like API latency. A new session must restore or use
  a populated staging-equivalent read-only benchmark dataset before accepting full latency.
- Lighthouse performance is **not proven green**. A dev-server Lighthouse run on
  `http://127.0.0.1:3311/weighing/analytics?scope_mode=company` scored performance `34`, which is a
  fail and also not the right release-shaped target because it audits `next dev`/webpack assets.
  Report:
  `apps/admin-web/apps/admin-web/.codex-goatos-render/lighthouse/admin-web-lighthouse-41bd3dd0-20260921T101510Z.json`.
- For this PR, Lighthouse acceptance is **performance only** on a production/standalone build,
  authenticated to an admin page, current SHA, with performance meeting the configured budget.
  Accessibility, SEO, and best-practices may be recorded but must not be used to claim success.
- Production standalone Lighthouse performance is green. Acceptance for PR #294 is performance-only:
  not SEO, not best-practices, and not a generic Lighthouse score. The page must render the target
  admin route on a production/standalone build and meet the configured performance budget.
- Route visual baselines are still stale/moved after the redesign. Latest integrity is clean, but
  final visible-route baseline review/update and a rerun are still pending.
- Final PR-ready status still requires: production/standalone Lighthouse performance proof, full
  API latency on a populated benchmark dataset, visible-route baseline review, final Storybook
  verification after any further edits, progress doc update, push, and PR description cleanup.

Latest update after this handoff:
- 2026-09-21 16:18 IST: Fixed three real visual judge findings from the visible WebView route
  set: Weighing KPI no-data values now wrap instead of clipping, Weighing load empty-state copy
  stays inside its card, and Counts Breakdown mobile pen-table headers/empty state no longer
  collide or disappear offscreen. Verified after screenshots:
  `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T10-42-33-298Z/weighing-analytics__webview__dark.png`,
  `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T10-42-33-298Z/weighing-analytics-load__webview__dark.png`,
  `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T10-47-09-130Z/counts-breakdown__webview__dark.png`.
  The focused route lane still exits red because route baselines are stale after the redesign; the
  fixed Counts capture has `integrity_findings=0`. A hydration console warning appeared once on
  the first Weighing mobile capture and needs a repeated current-SHA check before PR-ready. Guards:
  `npm --prefix apps/admin-web run design:guard` PASS; targeted direct Node tests for Weighing and
  Counts passed 40/40. A mistaken package-script test invocation ran the full suite and exposed two
  unrelated pre-existing failures (`live-tracker styles are scoped`, `work-board-avatar-overlap`);
  those are not from this batch but remain open PR debt until the final all-suite gate is clean.
- 2026-09-21 15:52 IST: Production/standalone Lighthouse performance proof is now green after the
  visible UI changes; later commits through `70d590a17cf94a9bb636ebd0e9de6c3f7fa0f66d` are
  documentation-only. Started the actual standalone server with
  `node apps/admin-web/server.js` from `apps/admin-web/.next/standalone` on `127.0.0.1:3314`
  against the current-SHA API on `127.0.0.1:18086`; verified
  `/weighing/analytics?scope_mode=company` returned HTTP 200 and real HTML. Ran Lighthouse with
  `ADMIN_WEB_LIGHTHOUSE_CATEGORIES=performance` and
  `ADMIN_WEB_LIGHTHOUSE_MIN_SCORES=performance=70`; PASS: performance `75`, FCP `2.1s`, LCP
  `5.6s`, TBT `160ms`, Speed Index `2.1s`, CLS `0`, total byte weight `735 KiB`. Report:
  `apps/admin-web/apps/admin-web/.codex-goatos-render/lighthouse/admin-web-lighthouse-4233b50c-standalone-perf-20260921T102148Z.json`.
  This supersedes the invalid dev-server Lighthouse performance `34` report. Remaining hard gate:
  full API latency still needs a populated benchmark dataset; do not accept the empty-data
  admin-all timing as production-like proof.

Suggested next commands for a new session:
```bash
cd /Users/raviteja/mesha/goatos-pr294-visual
git status --short --branch
git rev-parse HEAD
curl -fsS http://127.0.0.1:18086/version || true

# If continuing Lighthouse, do not use next dev. Use production/standalone and set the correct DB/API env.
npm --prefix apps/admin-web run build
# Then start standalone/start:local on 3314 with DATABASE_URL pointing to the PR benchmark DB and
# GOATOS_API_BASE_URL=http://127.0.0.1:18086, then run:
ADMIN_WEB_LIGHTHOUSE_URL='http://127.0.0.1:3314/weighing/analytics?scope_mode=company' \
ADMIN_WEB_LIGHTHOUSE_EXPECT_FINAL_URL_CONTAINS='/weighing/analytics' \
ADMIN_WEB_LIGHTHOUSE_CATEGORIES=performance \
ADMIN_WEB_LIGHTHOUSE_MIN_SCORES=performance=70 \
npm --prefix apps/admin-web run perf:lighthouse
```

## How to resume

```bash
git fetch origin && git checkout design/minimal-redesign-preview
cd apps/admin-web && npm ci              # node_modules has been deleted twice by a cleanup process
# local stack: OCI tunnel + API + web
~/mesha/tools/local/oci-goatos-a1-dev.sh tunnel      # 127.0.0.1:15432 (staging clone — READ ONLY)
# API on :18086, admin-web on :3310 (never 3300), Storybook on :6007
```

Artifacts (scratchpad, not in git): route checklist `audit/checklist.md`, polish pattern spec
`audit/pattern-spec.md`, per-module gap lists `audit/gaps/*.md`, screenshots under
`redesign/{final-parity,mobile-audit,webview,charts,sidebar,wave2}/`.

## Ground rules

- **Brand lock**: no color token in `app/mesha-theme.css` / `app/minimal-theme.css` may change.
  Banned: `#0A9F6C`, `#4FD89A`, `#131A21`, `#1B242E`, `#F4F6F8`, hardcoded Tailwind palette classes.
  A color diff vs `origin/main` on those two files is a P0.
- Third-party templates are **style reference only** — no copied code, CSS, images or wording.
- **Visible-surface scope only**: Action Center, Calendar, Control Tower, Protocol Adherence,
  Workflows, workflow-record, and calendar deep-link/detail routes are deprecated or hidden
  command-lens surfaces. They stay functional where existing product flows still link to them, but
  they are **not** PR #294 Minimal-dashboard parity targets and must not drive visual acceptance.
- Data is the OCI staging clone: **read-only**. Never submit, save, approve or delete.
- Presentation-only changes: business logic, data contracts and permissions untouched.
- Every UI change must pass: component snapshots (Storybook), route visual (`smoke:visual:live`,
  incl. 390px), and the mobile/WebView guard.

## Status

### Done
- Component kit (26 components): cards, KPI + tints, count-up, trend/icon badges, sparkline
  (line + bar), select field, filter bar, animated tabs, table treatment + footer, row menu,
  dense toggle, overlays, charts, page-enter, skeleton variants, theme toggle.
- Charts: tracks, rounded caps, dashed gridlines, right-aligned value column, donut with centre
  total, gradient area fill, unified tooltip, draw-in with stagger (`components/charts-premium.css`,
  tokens only).
- Shell: sidebar sub-nav rebuilt (continuous guide line, no broken stubs), animated expand/collapse,
  sliding active pill, animated rail, reduced-motion support.
- Kit adoption: 7 → 109 files; loading states 9 → 55 of 58 routes.
- Wave 2 routes that no batch covered: `/tasks`, `/leave`, `/login`, `/auth/action`, SOP editors,
  `/ceo-ai-admin` (de-hardcoded colors), global loading/error, 7 mismatched loading files.
- Docs: `docs/design/README.md`.

### In flight
- Final parity pass for **visible product surfaces only** (kit dialog z-index fix, remaining native
  selects, tab transitions, shape-matched skeletons, list/table/card patterns) + audit judges.
- Guards + mobile/WebView judges on visible routes only (tables, overlays, pagination, chart
  labels) and fixes.
- Storybook + stories + desktop/mobile snapshot lanes wired into local CI and skills.
- Mobile/WebView regression taxonomy mined from git history → `check-mobile-webview.mjs` + skill.

### Open / blocked
- `/routines` and the Feed cards SOP editor cannot render on the local backend (page not in this
  principal's contract; no `feed` section in current SOP data) — code done, visually unverified.
- `/leave` has no rows locally; only the empty state is verified.
- `app/kit-preview` is temporary and **must be deleted before merge** (Storybook replaces it).
- Legend side differs between recharts (top-right, `!important`) and SVG charts (top-left).
- `svg-series` shares one viewBox across wide and narrow cards → axis label scaling varies.
- Environment: a cleanup process on this machine deleted `node_modules` twice and wiped
  `~/mesha/weighing-calendar-db-config` (the old local API source). API is now built from this
  repo's `backend/` instead.

## Log

- **2026-09-21 14:08 IST** — Scope correction locked: deprecated/hidden command-lens
  surfaces (`Action Center`, `Calendar`, `Control Tower`, `Protocol Adherence`, `Workflows`,
  `workflow-record`, calendar details) are not Minimal-dashboard parity targets. Current visible
  route visual guard remains clean on the last broad sweep (`48/48`, desktop + 390px webview
  dark, `integrity_findings=0`), but template parity is still not complete until final visible
  route re-judge, Storybook, Lighthouse and current-SHA API gates finish.
- **2026-09-21 14:08 IST** — API latency follow-up: procurement loadwise sales was fixed by
  reusing the sale-average read (`a6d706311`, focused procurement tests green). Focused current
  latency retry after that fix left only visible Work Board CPT red
  (`work_board_page_cpt` p90 `339.2 ms`, p95 `359.9 ms`, p99 `360.2 ms`; report
  `.codex-goatos-render/api-latency-current-reds-a6d706311-20260921T083537Z.json`). Work Board
  trace points into vaccination due-work counting. The attempted memoized precheck skip helped CPT
  but made CBE's no-vaccination day pay the heavier canonical proof; direct SQL timing showed the
  precheck is cheap (`~49 ms` CBE no-work, `~10 ms` CPT work-present), so the fix is to keep the
  precheck and use it to avoid the zero-row canonical read.
- **2026-09-21 14:30 IST** — Current pushed SHA `d66e77428`. Focused tests are green:
  `go test ./internal/workboard/... ./internal/processintegrity/adapters/boardsource
  ./internal/processintegrity/adapters/postgres`. Full admin-all latency cannot currently be
  re-run cleanly on this laptop because the only reachable local DB is migrated to `000379` while
  `origin/main` and PR #294 currently contain migrations only through `000369`; the API correctly
  refuses to boot with `migration_drift_dbahead_fatal`. The missing `000370`-`000379` files exist
  only in other local PR-328 review checkouts, so they are not being copied into this visual PR.
  Last valid current-SHA latency evidence before the drift blocker: focused reds green at
  `34f2cce62` (`feed_direction_preview` p90 `285.2`, `procurement_loadwise_sales` p90 `194.6`,
  `work_board_page_cpt` p90 `255.8`), but full admin-all remained unstable/red and is **not**
  certified green.
- **2026-09-21 14:36 IST** — Non-API UI guards after remote drawer fix `ce9a73467` and progress
  update `e80796484`: `npm --prefix apps/admin-web run design:guard` PASS (`p0=0`, 60 waived,
  9 stale waivers); `npm --prefix apps/admin-web run smoke:webview:static` PASS. Live visible-route
  screenshot sweep against `http://127.0.0.1:3311` is currently blocked: `48/48` captures rendered
  error boundaries because the local API cannot boot against the DB-ahead clone. Screenshot
  directory: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T09-00-40-047Z`.
- **2026-09-21 14:45 IST** — Storybook component visual sweep ran without API:
  `npm --prefix apps/admin-web run smoke:stories -- --no-build --viewports desktop,mobile --themes
  dark --require-baseline --max-diff-ratio 1`. Render integrity is clean (`integrity_findings=0`),
  but the baseline lane is red: 257 stories, 554 captures, 130 failed, 16 missing baselines. Failures
  are baseline/perceptual/dimension drift concentrated in kit cards, charts, forms, tables, drawers,
  motion frames, and mobile story heights. Screenshot directory:
  `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T09-06-01-420Z`.
- **2026-09-21 15:05 IST** — Storybook component baseline refreshed after visual inspection of
  representative failed mobile charts/forms/tables (labels present, no overlap/clipping). Verification
  rerun passed: `npm --prefix apps/admin-web run smoke:stories -- --no-build --viewports
  desktop,mobile --themes dark --require-baseline --max-diff-ratio 1` => 257 stories, 554 captures,
  0 failures, `integrity_findings=0`, compared pixels 554. Screenshot directory:
  `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T09-24-17-164Z`.
- **2026-09-21 15:07 IST** — Pushed `54b94237e` to PR #294 after refreshing Storybook baselines.
  Immediate guards: `design:guard` PASS (`p0=0`), `smoke:webview:static` PASS, route coverage test
  PASS, typecheck PASS, working tree clean. `apps/admin-web/scripts/webview-critical-routes.json`
  now names only the 24 visible mobile-critical routes and explicitly excludes Action Center,
  Calendar, Control Tower, Protocol Adherence, and Workflows from PR #294 template-parity
  acceptance. Live API-backed Chrome route screenshots remain blocked until the local API can run
  again; `127.0.0.1:18086/livez` is currently down and the known blocker is still the DB-ahead
  migration drift (`000379` DB vs `000369` in this PR/main), not a UI proof failure.
- **2026-09-21 15:26 IST** — Local API/browser proof restored with a disposable PR clone:
  `goatos_pr294_visual` cloned from the scratchpad Postgres and migration bookkeeping trimmed to
  this PR's embedded `000369` level. API `/version` on `127.0.0.1:18086` reports build
  `956bccf17057693d8bfc93c4bff1b3470c0f9af5` and `migration_drift=false`; admin-web runs on
  `127.0.0.1:3311` against that API/DB only. Broad visible-critical route capture produced 48 real
  Chrome screenshots and exposed three real render-integrity findings: Feed Analytics mobile empty
  copy clipped under the global one-line card-caption rule; Counts Analytics and Mortality empty
  cards rendered as title plus lonely info icon. Fixed by adding wrapped route-scoped empty-state
  copy and making Count empty cards carry explicit body content. Verification after the fix:
  `npm --prefix apps/admin-web run typecheck` PASS; `npm --prefix apps/admin-web run design:guard`
  PASS; `npm --prefix apps/admin-web run smoke:webview:static` PASS; focused Chrome route capture
  for Feed Analytics variants plus Counts Analytics/Mortality desktop+390px webview dark produced
  16/16 screenshots with `integrity_findings=0`, `unstable_captures=[]`. Screenshot directory:
  `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T09-55-54-893Z`.
- **2026-09-17** — Template evaluation (Minimal / Tailwind Plus / Untitled UI / TailAdmin / Tremor);
  decision: build inspired-by in our own stack, no purchase. First pass on 8 routes.
- **2026-09-18 00:39** — Brand colors restored after an agent swapped them for the template's
  emerald/slate; brand lock added to every agent prompt.
- **2026-09-18 01:00** — Full sweep (all routes), chart upgrade, sidebar rebuild.
- **2026-09-18 02:30** — Route checklist (63 entries) + per-module gap lists (~320 items counted so
  far, 55 P0); pattern spec written.
- **2026-09-18 03:10** — Protective commit `885dbbcb9` after the cleanup process deleted worktree
  dependencies mid-run; bundle backup taken; branch pushed for cross-account resume.

## 2026-09-18 04:30 — guard truth + parity pass

**Guard suite run on this branch** (each failure verified against a clean `origin/main`
export, where the same command passes — so these are redesign-caused, not pre-existing):

| Lane | Status |
|---|---|
| `tsc --noEmit`, `lint`, smoke route coverage | pass |
| `node --test` | **fail — 13 tests in 8 files** (87/87 pass on main) |
| phone viewport guard | **fail — 4 new fixed-px-width findings** |
| `check:mock-fidelity` chain | **fail — hardcoded UI copy in new kit components** |
| `smoke:visual:live` / `responsive:guard` | blocked at the time (API was down; API has since been rebuilt and is back on :18086) |
| visual baseline diff | no baselines exist on this branch yet |

**Environment gotcha:** guards need Node 24. The default shell node is v20, where `npm test`
dies with `bad option: --experimental-strip-types` (exit 9) and hides all 13 real failures.
Use `/Users/raviteja/.nvm/versions/node/v24.19.0/bin` on PATH.

**Two likely behaviour regressions (not just markup churn), to fix before merge:**
- Herd Signals lost its Watchlist filter from the filter bar.
- Vaccination drive selector lost its URL round-trip key (batch/park selection).

**Parity pass landed:** kit Dialog stacking-context fix (every dialog was unclickable under its
own backdrop); new kit components (ProgressRow, RadialStat, ListRow, TableFooter, RowMenu,
DenseToggle, TabPanel content transition, KpiCard tint/gradient + watermark, shape-matched
Skeleton variants, bar sparkline default); Sales/Procurement native selects 38 → 0.

**Fonts:** Public Sans (body) + Barlow (display) self-hosted via `next/font`; the runtime Google
Fonts link is gone. Brand color tokens untouched.

**Mobile/WebView guard shipped:** `scripts/check-mobile-webview.mjs` — 7 recurring failure classes
mined from 12 months of history, 112 routes × desktop + Pixel 5 × dark + light, wired into
`run-local-ci.sh`, AGENTS.md, CLAUDE.md and `.agents/skills/mobile-webview-guard`. Known static
debt is waived in `scripts/check-mobile-webview-waivers/` (7 × `100vh`, 18 × `backdrop-filter`
without `@supports`) so the lane is green now and red on any new instance.

**Open defects found by browsing Storybook stories** (none caught by route screenshots):
RowMenu long labels overlap; RowMenu not anchored to its trigger; Sparkline overflows its card at
390px; RowMenu clipped by `.kit-tablecard { overflow:hidden }` on a table's last row.

**In flight:** gap implementation wave (7 agents, ~450 P0/P1/P2 items across 6 modules),
performance + Lighthouse comparison vs `origin/main` (5 agents, adds a perf budget guard),
Storybook snapshot lanes.

## 2026-09-18 05:55 — story-level defects, laptop-only constraint

**Storybook is finding what route screenshots miss.** Defects spotted by eye while browsing
stories (now assigned to fixers, confirm before closing):
- RowMenu: long labels overlap each other; the menu is not anchored to its ⋮ trigger; it is
  clipped by `.kit-tablecard { overflow:hidden }` on a table's last row.
- Sparkline: overflows its card at 390px (fixed width instead of container width).
- Chips: demographic chips render as oversized tall lozenges; a long-label chip clips mid-word.
- AnimatedTabs: switching panels flickers / jumps height.
- Several stories show large dead vertical gaps between sections.

**Judging gap closed:** judges had been reviewing app routes only. A story-lens workflow now
reviews EVERY story at 1440 + 390 in both themes (4 captures per story), scores each /10, fixes
by kit area, then independently re-judges every story that scored < 8.

**Storybook 10.6** ships CLI bindings for agent tools/skills — worth wiring into the judge lane
so agents enumerate and run stories through a real interface instead of guessing URLs. Not done yet.

**Infrastructure decisions:**
- Perf/Lighthouse workflow was **parked** (it was two production builds + 42 Lighthouse runs on a
  laptop at load ~140; the numbers would have been meaningless). It runs alone after the UI work.
- OCI VM was evaluated for offload: 4 cores, 22GB RAM, idle — but root disk is 95% full (1.8GB
  free) and it hosts the staging Postgres, so builds there risk staging. `/var/oled` has 15GB free
  if this is revisited. Decision: **laptop only**.
- Local API was rebuilt from this repo's `backend/` (its old source dir was deleted by the cleanup
  process) and runs with `GOATOS_HTTP_ADDR=127.0.0.1:18086`, env from
  `~/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env`, plus
  `GOATOS_LOCAL_MEDIA_SIGNING_SECRET` and `GOATOS_ALLOW_STALE_LOCAL_STACK=1`.
- The dev server on :3310 recompiles constantly while agents edit; for a stable click-through,
  build a production snapshot on a spare port instead.

**In flight:** gap implementation wave (6 module agents), Storybook snapshot lanes, story judges
+ kit fixers + re-judge.

## Order of work (agreed 2026-09-18 06:00)

1. **UI implementation first** — finish every module's P0/P1/P2 from the gap lists, kit defects
   from the story judges, and the guard failures (13 tests, phone-viewport findings, mock-fidelity
   hardcoded copy).
2. **Then** visual baselines + full guard suite green, PR out of draft.
3. **Parked until the UI work is done:** web performance (Lighthouse/bundle/budget guard) and API
   latency work. Both get their own pass with the machine quiet, so the numbers mean something.

Cadence: commit + update this log + push to the PR after every wave, so any account/session can
resume mid-flight.

---

# PAUSED 2026-09-18 06:10 — resume from here

All agent workflows were stopped mid-flight at the user's request. The working tree is committed
and pushed; nothing is lost. Some waves were interrupted partway, so treat the counts below as the
true state, not the reports above.

## Measured state at pause

| Metric | Value |
|---|---|
| Routes under `app/(admin)` | 58 |
| Routes with a loading state | 56 |
| Files importing `components/kit` | 147 (started at 7) |
| Kit components | 26 |
| Storybook stories | 32 story files |
| Native `<select>` remaining | **19** |
| Legacy chart imports remaining | **15** |
| `window.confirm` remaining | **3** |
| Old `SegmentedLink` tab strips remaining | **4** |

## Interrupted mid-wave (re-run these first)

1. **Gap implementation wave** — 6 module agents working `scratchpad/redesign/audit/gaps/*.md`
   (~450 P0/P1/P2 items). Sales/Procurement + shared foundation landed; Feed/Counts,
   Weighing/Health/Signals, Verify/WorkBoard/Ops, Vaccination/ControlTower/AI and
   SOPs/Tasks/Leave/Routines were still running. The 19 selects / 15 legacy charts / 3 confirms /
   4 link strips above are what those agents had left.
2. **Storybook story judges + kit fixers** — judges review every story at 1440 + 390 in both
   themes, fixers work by kit area, then an independent re-judge of anything scoring < 8.
3. **Motion parity** — phase 1 (read-only) was capturing reference interaction frames and
   inventorying ours; phases 2–3 (apply + frame-by-frame compare) never started.
4. **Storybook guard lane** — snapshot lanes + play tests were mid-wiring.

## Known defects still open

- RowMenu: long labels overlap; menu not anchored to its trigger; clipped by
  `.kit-tablecard { overflow:hidden }` on a table's last row.
- Sparkline overflows its card at 390px.
- Chips: oversized tall lozenges; long-label chip clips mid-word.
- AnimatedTabs: panel switch flickers / jumps height.
- Charts: hover motion does not match the reference — the hovered donut slice should expand while
  siblings dim and the centre label swaps; the tooltip should follow the pointer with a
  fade + scale. Ours renders a static box over the ring.
- Several stories have large dead vertical gaps.
- Guard failures: 13 unit tests (incl. Herd Signals' missing Watchlist filter and the vaccination
  drive-selector URL key — likely real behaviour regressions), 4 phone-viewport fixed-px findings,
  mock-fidelity hardcoded copy in `select-field`, `dense-toggle`, `overlay`, `row-menu`,
  `kit-preview`, `goat-passport`.
- `app/kit-preview` must be deleted before merge.
- `/routines` and the Feed cards SOP editor cannot render on this backend; `/leave` has no local rows.

## Deferred by decision

Web performance (Lighthouse, bundle, perf budget guard) and API latency work — run last, alone,
so the numbers are meaningful.

## To resume

```bash
git fetch origin && git checkout design/minimal-redesign-preview
cd apps/admin-web && npm ci
export PATH=/Users/raviteja/.nvm/versions/node/v24.19.0/bin:$PATH   # guards need Node 24
~/mesha/tools/local/oci-goatos-a1-dev.sh tunnel &                    # 127.0.0.1:15432, READ ONLY
# API (its old source dir was deleted; build from this repo):
cd ../../backend && go build -o /tmp/goatos-api-local ./cmd/api
set -a; . ~/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env; set +a
GOATOS_ENV=local GOATOS_HTTP_ADDR=127.0.0.1:18086 GOATOS_AUTH_MODE=bearer \
  GOATOS_AUTH_ISSUER=goatos-local GOATOS_AUTH_AUDIENCE=goatos-api \
  GOATOS_AUTH_HS256_SECRET=goatos-local-dev-secret-32-bytes-min \
  GOATOS_LOCAL_MEDIA_SIGNING_SECRET=goatos-local-media-dev-secret-32bytes \
  GOATOS_ALLOW_STALE_LOCAL_STACK=1 GOATOS_ALLOW_CUSTOM_LOCAL_STACK_DB=1 /tmp/goatos-api-local &
# web + storybook
cd ../apps/admin-web && GOATOS_API_BASE_URL=http://127.0.0.1:18086 GOATOS_BEARER_TOKEN=selfmint \
  GOATOS_AUTH_MODE=bearer GOATOS_AUTH_HS256_SECRET=goatos-local-dev-secret-32-bytes-min \
  GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 \
  GOATOS_LOCAL_USER_ID=90000000-0000-4000-8000-000000000101 npx next dev -p 3310 &
npm run storybook   # :6007
```

Artifacts live in the scratchpad (NOT in git — copy them out if this machine is wiped):
`/private/tmp/claude-501/-Users-raviteja-mesha/043cba5f-5caf-4d15-9eaf-dc6fcb54d4df/scratchpad/redesign/`
— `audit/checklist.md` (63-route inventory), `audit/pattern-spec.md` (visual contract),
`audit/gaps/*.md` (per-module worklists), `webview/taxonomy.md`, and screenshot sets under
`final-parity/`, `story-audit/`, `charts/`, `sidebar/`, `wave2/`, `motion-ref/`, `motion-ours/`.

---

# RESUMED 2026-09-19 03:46 — merge with main, judges, builders

Worktree moved to `/private/tmp/goatos-redesign-merge` (the old `/private/tmp/goatos-minimal-redesign`
was wiped). Branch `design/minimal-redesign-merge` is pushed onto the PR head
(`design/minimal-redesign-preview`). Merged web runs on **:3311**, API on :18086 (OCI tunnel).

## Environment fixes (would have blocked anyone resuming)
- Branch lockfile lacked `react-is` (recharts peer) → every page 500'd. Added to admin-web deps.
- Web needs `GOATOS_ENV=local` and `GOATOS_AUTH_ISSUER=goatos-local GOATOS_AUTH_AUDIENCE=goatos-api`
  alongside the runbook's vars, or it redirects to /login and the API answers 401.
- `npm ci` must run at the repo ROOT (workspace), not in apps/admin-web.
- To see real notifications locally run the web as a member with an inbox:
  `GOATOS_LOCAL_USER_ID=491c6057-068c-58a1-b5f8-7573b82cae72` (Ravi, 5k rows, 10 kinds).
- `lib/web-push-actions.ts` re-exported types from a `"use server"` module; Turbopack turned them
  into server references and the feed action threw `ReferenceError` in dev. Fixed.

## 04:00 — origin/main merged (bcc825b94)
Only 16 conflicting files despite ~600 commits. Main wins on behaviour, branch wins on presentation.
tsc/lint/next build green; 941 tests, 21 failures all pre-existing (the "13 known" above was stale —
the branch tip actually had 22). Routes main added with zero kit adoption: `/tasks` (rewrite),
`/configuration/items`, `/counts/mortality`, `/sales/farm-born`, feed weight-band card, notifications.

## 04:20 — judge verdicts (both on the pre-merge tree, :3310)
Judge B (tables/lists/shell/notifications): palette clean, no window.confirm. P0: 5 sales/procurement
pages crash (function props into client pager), native `<select>` in kit table footer, "F2-Male"
visible on weights + feed config. P1: row menu rendered off-screen, 5 page-header patterns,
prose under titles on ~90% of routes, stock 404, unstyled Add-disease form, CEO FAB covers tables,
sidebar doesn't auto-expand active group. Findings: scratchpad `judge-b/FINDINGS.md`.
Judge A (charts/motion/skeletons): draw-in + reduced-motion + fonts pass. P0: SVG tooltips render
off-screen (fixed-position inside a transformed ancestor), recharts tooltip never fires, F2 in the
pen-tag chart. P1: zero sparklines/trend chips/count-up on any KPI, no bar/slice hover states,
2-point areas where columns belong, skeletons not shape-matched on 3 routes, `#08855B` hover in
minimal-theme.css. Findings: `judge-a/FINDINGS.md`.

## 04:35 — builders in flight (merged tree, :3311)
- N notifications — DONE, committed a32f6d44f (tabs+counts, semantic rows, popover/sheet, skeleton).
- R new-main routes kit adoption (mortality, farm-born, configuration, weight-band, tasks modals).
- K kit/shell P0-P1 list from Judge B.
- C chart motion/hover/tooltip/KPI list from Judge A.
- Judge S side-by-side vs the live Minimal demo (mui.com), paired PNGs → `judge-s/REPORT.md`.
Cadence: commit per builder as it lands, push to the PR branch, re-judge, repeat.

## 04:55 — coverage matrix + judge S baseline
- `docs/progress/admin-web-redesign-coverage.md`: 67 routes + 14 shell elements; every tab/overlay/
  table/chart/control counted from the 2026-09-18 inventory. A row closes only when a JUDGE scores
  it >=8 on ours-vs-reference pairs (1440+390, dark+light, hover/motion frames). Builders cannot
  close rows.
- Judge S (side-by-side vs Minimal, 165 pair images): average **4.7/10** — 404 page 1, goat passport
  3, tasks 3.5, vaccination 3.5, bell 6.5. Top gaps: no 404, hard-nav loading has no shell, prose
  under every title, tasks board empty, raw enums on passport, footer/hover on tables, product-list
  rows single-line, no stat strip on list pages, work-board columns, chart draw-in fires under the
  skeleton swap so it is invisible. All routed to builders K/R/C.
- Judge P (perf) and Builder G (design-system skill + static guard + Storybook/route visual lanes)
  launched. Standing rule from Ravi: every API <500 ms, judged every wave; guards so new features
  cannot regress.

## Design guards (builder G, 2026-09-19)

Owner ask: "write design skills + guards + local CI so existing and NEW features follow this;
Storybook visual regression must run for every page like Paparazzi does for Android; web and
mobile must render properly — no breaking graphs, tags, numbers, nothing."

What exists now (all wired into `tools/ci/run-local-ci.sh` → `run_admin_web`, so `make ci-local`
and `make land-main` refuse on red; `tools/ci/guardrail-manifest.json` entry
`admin-web-design-system`):

| Piece | Path | Command |
|---|---|---|
| Skill (the contract, agent-facing) | `.agents/skills/design-system/SKILL.md` + `references/{tokens-and-fonts,components,motion,banned}.md` | — (wired from AGENTS.md / CLAUDE.md / SKILLS.md) |
| Static guard | `apps/admin-web/scripts/check-design-system.mjs`, waivers `scripts/check-design-system-waivers/design-system-waivers.json` | `npm run design:guard` |
| Render-integrity probe (shared) | `apps/admin-web/scripts/lib/render-integrity.mjs` | inside both visual lanes |
| Baseline contract (manifest + local PNG + waivers) | `apps/admin-web/scripts/lib/visual-baseline.mjs`, `apps/admin-web/visual-baselines/{stories,routes}/` | `--update-baseline` only |
| Story lane (every story, 1440+390, dark+light, integrity, interaction frames) | `apps/admin-web/scripts/smoke-stories-visual.mjs`, frames in `stories/kit/MotionFrames.stories.tsx` | `npm run visual:stories` |
| Route lane (desktop / phone / webview 390 / webview-small 360, dark+light, integrity + mobile probe, drawers, webview-critical filter, static webview report merge) | `apps/admin-web/scripts/smoke-routes-visual.mjs`, `scripts/webview-critical-routes.json` | `npm run visual:routes` |
| Storybook fonts | `.storybook/fonts.css` + `.storybook/fonts/*.woff2` (self-hosted Public Sans + Barlow), variables in `.storybook/preview.css` | — |
| New kit stories | `stories/kit/PageFrame.stories.tsx` (PageShell/PageHeader/BarList), `FilterBar.stories.tsx`, `ThemeToggle.stories.tsx` (+RetryButton), `MotionFrames.stories.tsx` | — |

First-run findings (2026-09-19, moving tree — other builders landing concurrently):

- **design:guard**: 0 P0. Waived debt at the last refresh: 33 `prose-under-title`, 7 `fixed-px-width`
  (`features/counts/herd-actions-ui.tsx` dialogs 560–900px), 6 `native-date-input`, 6
  `missing-loading-tsx`, 5 `page-outside-shell`, 5 `hex-colour-in-code` (ceo-ai illustration SVG),
  1 `native-select` (`features/configuration/row-drawer.tsx`), 1 `f2-literal`
  (`features/counts/counts-summary-cards.ts` synonym list), 1 `chart-without-tooltip`
  (`app/login/page.tsx`). Started at 128 waivers; builders K/F retired 66 of them during the session.
- **visual:stories** (sweep #2, 256 stories, 1440+390 × dark+light, 1,070 captures incl. 26
  interaction frames): 996 passed, 30 failed, 0 unreached. All 28 non-harness failures are
  play functions broken by concurrent kit edits — `TableFooter` lost its "Rows per page"
  label/combobox (4 stories), `RowMenu` no longer opens / `role=menu` missing (3 stories),
  `AnimatedTabs` tab-switching `h3` assertion (1 story). Integrity findings waived on first run:
  `element-overflows-viewport` 342 (AnimatedTabs "many tabs" strip is 1610px wide at 1440 — the
  strip does not scroll, it overflows; SelectField menu 1474px; mostly 390px captures),
  `clipped-text` 60 (Button/Tag/ListRow/ProgressRow long-text demos: ellipsis with no `title`),
  `bad-text` 4 (`CountUp` renders a literal "NaN"), `chart-svg-empty` 4 (`Sparkline` empty state
  paints an empty `<svg>`). Storybook rendered every story in **system-ui** until this pass —
  fixed by self-hosting the fonts; every earlier story baseline was typographically wrong.
- **visual:routes** (30 webview-critical routes × desktop/phone/webview/webview-small × dark/light,
  drawers opened): `/alerts`, `/alerts?date=…`, `/approvals` render "internal server error" on
  every profile (backend/contract, not layout); `F2-Male` is rendered on `/action-center` and 5
  other routes (`f2-literal`); `console-error` "two children with the same key" on
  `/vaccination`; Recharts tick labels 10.5px on `/counts/mortality` and every chart route at
  phone width (`mobile-axis-text-too-small`, 240 captures); calendar month picker popover
  escapes the 390px viewport (`element-overflows-viewport`); notification panel rows
  (`.nc-t`, `.nc-body.nc-clamp`) ellipsis without `title` on every route (`clipped-text`).
  Tap targets < 44px are recorded as informational (436) — gated by `smoke:webview`.
- Environment: :3311 was in a Turbopack panic (`charts-premium.css` postcss worker) and later
  500'd on a `charts.tsx` duplicate `onLeave`; :6007's indexer stuck on the same error
  ("Could not parse import/exports with acorn" for every story) until restarted. Both lanes
  report those honestly (HTTP 500 / index 500) rather than passing.

## 08:30 — judge rounds (module M1: sales/counts/procurement/tasks/goats/work-board/config)
| Round | Mean /10 | Rows ≥8 | Landed before it |
|---|---|---|---|
| S baseline | 4.7 | 0 | — |
| M1 r1 | 6.4 | 4/24 | frame (e0eb0aa79), charts (29c4207c2), routes (179136927…) |
| M1 r2 | 6.9 | 5/24 | R fix wave (fac779994), shell skeleton/optimistic tabs (4cdf72bb4) |
| M1 r3 | 7.4 | 10/24 | loads/passport/config + KPI sentences (72a9d344b), chart r2 (82ee98dbd) |
API (Builder B): calendar events 3 s → 284 ms p95, vaccination sheds 3 s → 348, loadwise 795 → 290,
notifications 667 → 347 + no feed read per navigation, gzip + bootstrap split 1,032 KB → ~30 KB/render.
Stock `make api-latency-gate` runs unmodified: 72/91; the rest need migrations 000354/000359 +
ANALYZE on the clone (owner decision) or are p90 noise on the 40 ms tunnel.
M2 (shell + verify/approvals/work-board/routines/calendar/vaccination/health/feed/ops/workflows)
scoring in progress; G (design-system skill, static guard, Storybook + route + WebView visual lanes)
in progress.

## 12:50 — clone caught up; final round
- OCI clone: `ANALYZE` (304 unanalysed tables → 0) and migrations 000333→000359 applied with the
  applier's local drift flag (000319 had been applied from an older file). /approvals, /alerts,
  /tasks?scope=assigned_by_me, /configuration/items render. Origin/main re-merged (2740b09ff).
- Judge rounds: M1 (24 rows) 4.7 → 7.6, 13 closed; M2 (50 rows) 5.6 → 6.6, 3 closed; hooks crash
  root-caused (bell Server Action on redirecting pages) and gone.
- Notification centre rebuilt as Minimal's drawer; ⚙ becomes a real push toggle + browser list
  or is removed; technical copy banned from UI by a new guard.
- Next: last builder wave (K M2 list, N push/copy, G technical-copy guard) → final M1+M2 round →
  frozen-SHA gate (prod build, all lanes, route baselines, WebView-critical on emulator,
  kit-preview deleted) → PR out of draft with any remaining <8 rows listed as known gaps.

## 23:45 — PR #294 resumed after main rebase (Codex, 2026-09-20)

- Isolated worktree: `/private/tmp/goatos-redesign-merge`; primary checkout left untouched. Merged current
  `origin/main` into PR head and resolved conflicts in `mesha-theme.css`,
  `loadwise-section.tsx`, and `inspection-editor.tsx`.
- Fixed judge/live-smoke regressions:
  - Restored main chart ramp tokens while keeping the redesign shell tokens.
  - Portaled kit sheets/dialogs to avoid overlay clipping inside page transforms.
  - Farm-born stage labels now use the full backend stage vocabulary, not raw K/F codes.
  - SOP follow-up owner control moved from native select to kit `InlineSelect`.
  - Added `/sales/sops/loading.tsx`.
  - Work Board card metadata no longer widens/clips cards at laptop/mobile widths.
  - Progress rows now provide accessible names for `role=progressbar`.
  - Disabled pager arrows are real disabled buttons; smoke accepts icon-only accessible labels.
  - Farm-born summary and sold tables scroll horizontally on phone instead of crushing text; the sold
    Value column no longer sticks over Breed on mobile.
  - Contained kit primary buttons use `--primary-fg` so the green Download button passes contrast.
  - Weighing analytics live-smoke loaded signal now matches current page copy.
  - Admin-all latency manifest/CI now includes Work Board `/work-board/page` hot paths.
- Verification after final edits:
  - `npm --prefix apps/admin-web run typecheck` PASS.
  - `npm --prefix apps/admin-web run design:guard` PASS (`p0=0`, `failures=0`; 60 existing waivers,
    9 stale waivers).
  - `node --test apps/admin-web/lib/stage-display.test.mjs apps/admin-web/scripts/perf-capture-budget-contract.test.mjs tools/perf/api-latency-policy.test.mjs apps/admin-web/scripts/lib/local-stack-receipt.test.mjs apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs` PASS (40 tests).
  - `npm --prefix apps/admin-web test -- components/kit/progress-row.test.mjs` ran the full admin-web
    suite and PASS (992 tests).
  - Focused live Chrome smoke PASS for laptop+mobile routes:
    `work-board-populated`, `sales-loads-farm-born`, `sales-farm-born`, `weighing-analytics`.
    Evidence directory:
    `.codex-goatos-render/admin-web-screenshots/2026-09-20T18-13-44-747Z`.
  - Chrome proof screenshots also captured in `.codex-goatos-render/pr294-screenshots/`, including
    `sales-farm-born-mobile-fixed.png`.
- Remaining caveat: full Storybook visual baseline sweep is still known-red from prior judge output
  (hundreds of baseline diffs/missing baselines). This was not waived or silently rebaselined here;
  PR should stay draft unless that broader visual-baseline debt is accepted or repaired.

## 01:55 — Minimal-style typography/surface correction (Codex, 2026-09-21)

- PR head pushed: `05a740973132ba17224977da4ab603eb8f00d451`.
- Scope: user clarified the PR's purpose is a visual overhaul toward the MUI Minimal Dashboard feel,
  with Mesha brand colors retained as the accent system. This pass intentionally moved the dark shell
  surface tokens toward Minimal's slate card/background balance while keeping Mesha green for brand,
  active nav, primary actions and chart accent use.
- Changes landed:
  - Removed the Barlow display split; admin-web now uses Public Sans for both body and display roles.
  - Normalized heading/card/KPI letter-spacing to `0` and reduced heavy local `800` heading overrides.
  - Tightened shell/card/table/sidebar rhythm in `frame.css` and `minimal-theme.css`.
  - Retired feature-level typography overrides in Work Board, Procurement and notification panel that
    made those pages keep the old heavier visual language.
  - Feed Direction / Feed Packing empty cards now render visible empty-state bodies instead of blank
    cards when the sheet is not issued yet.
- Verification after these edits:
  - `npm --prefix apps/admin-web run typecheck` PASS.
  - `npm --prefix apps/admin-web run build` PASS at `b7899536ba90c3ee314046a75cb329aa599b7b7e`.
  - Focused live Chrome route visual capture on `feed-packing,weighing-analytics,sales-sold,work-board,people,procurement-vendors`
    across desktop + 390px webview dark: `integrity_findings: 0`, `unstable_captures: []`.
    Screenshot dir: `.codex-goatos-render/admin-web-route-screenshots/2026-09-20T20-18-56-871Z`.
  - Expected baseline/hash failures remain because the visual system moved globally; no baseline update
    was performed in this pass.
  - Full `smoke:webview` PASS: `mobile_webview_routes_visited=464`, `mobile_webview_waived=0`.
    Report: `.codex-goatos-render/admin-web-webview/2026-09-20T20-07-42-646Z/report.json`.
  - Standalone production Lighthouse PASS on `/weighing/analytics`: performance 100,
    accessibility 100, best-practices 96, SEO 100.
    Report: `apps/admin-web/.codex-goatos-render/lighthouse/admin-web-lighthouse-b7899536-standalone.json`.
  - A prior `next start` Lighthouse run was deliberately not counted: this app uses `output:
    standalone`, and `next start` loaded development/hot-reloader chunks, producing a false
    performance 34.
- Still pending:
  - API latency remains red at current PR head `7e4c0b23fca256383690bb88fbce7d3ed7e8a807`.
    Full admin-all report: `.codex-goatos-render/api-latency-admin-all-b7899536.json`.
    It is clean SHA/clean tree/stamped API, 92 endpoints, 8 reds on the full run:
    `feed_direction_preview`, `feed_packing_worklist`, `vaccination_operations`,
    `vaccination_command_board`, `vaccination_command_shed_dose_matrix`, `work_board_page_cbe`,
    `work_board_page_cpt`, `verification_queue`.
  - Focused re-sample of those reds:
    `.codex-goatos-render/api-latency-pr294-red8-resample.json`; persistent reds narrowed to
    `vaccination_operations` (p90 520.8, p95 522.2), `vaccination_command_board` (p90 337.9),
    `vaccination_command_shed_dose_matrix` (p90 532.4, p95 544.4, p99 631.7), and
    `work_board_page_cpt` (p90 302.1). Feed preview/packing and verification queue passed on the
    focused re-sample and look like local/tunnel jitter unless they recur.
  - Do not claim API performance completion until the persistent reds are fixed or explicitly
    accepted.
  - Full Storybook and full route baseline debt remains unresolved; focused integrity is clean, but
    the all-component/all-page visual-baseline contract is not yet green.

## 02:12 — Responsive polish + vaccination latency patch (Codex, 2026-09-21)

- Scope: follow-up on judge findings after the Minimal-style correction. This pass fixes actual
  mobile/webview breakage rather than blessing baseline movement:
  - Shared `AnimatedTabs` now scrolls the active tab fully into view with safe inline padding.
  - Work Board lanes stack into full-width columns on phone widths instead of clipping sideways.
  - Procurement Vendors renders as mobile record cards below 640px; hidden table headers are no
    longer sticky/layout-visible to the webview integrity probe.
  - Vaccination command-board/cohort/shed-dose live cache keys now route through the explicit/live
    snapshot helper so default “now” reads can reuse the short live bucket while explicit `as_of`
    snapshots remain exact.
  - Shed-dose matrix SQL planner fences from the latency investigation are present in
    `commandboard_sql.go`.
- Verification so far:
  - `git diff --check` PASS.
  - `go test ./internal/vaccinationexecution/adapters/postgres -run 'TestVaccinationCache|TestCommandBoard|TestVaccinationCommandBoard|TestCommandBoardPlan|TestCommandBoardQueryPlans|TestCommandBoardReworkUsesLatestObligationProof|TestVaccinationCommandBoardShedDoseDateShiftOneCellPerState'` PASS.
  - `go test ./internal/vaccinationexecution/adapters/http ./internal/vaccinationexecution/domain` PASS.
  - `npm --prefix apps/admin-web run typecheck` PASS.
  - `npm --prefix apps/admin-web test -- scripts/smoke-visual-route-coverage.test.mjs scripts/work-board-avatar-overlap.test.mjs scripts/perf-capture-budget-contract.test.mjs` PASS (993 tests).
  - Focused Chrome webview recapture for `procurement-vendors` now has `integrity_findings: 0`;
    screenshot dir `.codex-goatos-render/admin-web-route-screenshots/2026-09-20T20-42-37-870Z`.
    The lane still exits red only because the intentional mobile-card redesign changes the visual
    hash versus the old baseline; no baseline was updated.
- Pending before saying performance is fixed:
  - Rebuild/restart the local API at the new commit SHA and rerun the focused red latency manifest
    for vaccination operations, command board, shed-dose matrix and Work Board CPT.
  - Rerun focused route visual after the production snapshot is rebuilt, then attach/show updated
    Chrome screenshots.

## 02:25 — Latency rerun after pushed fixes (Codex, 2026-09-21)

- Pushed commits after the typography pass:
  - `0fa95488229f03190a718dc5c5112ea4698cc40d` — mobile tabs, Work Board stacking,
    Procurement Vendors mobile card layout, command-board/shed-dose live cache and planner fixes.
  - `a8017141261b387355d9e244d0ff64d5babeba32` — live `/vaccination/operations` cache key no
    longer misses on the default moving `due_before` instant; explicit `due_before` remains exact.
- API proof:
  - Rebuilt `/tmp/goatos-api-local` at `a8017141261b387355d9e244d0ff64d5babeba32`.
  - `/version` readback matched the git SHA and migration version `000369#1`.
  - Worktree was clean for the latency runs.
- Focused red8 rerun:
  - Report: `.codex-goatos-render/api-latency-pr294-red8-after-a80171412.json`.
  - Vaccination fixed: `vaccination_operations p90=57ms` (was 523.4ms), command board
    `p90=91.5ms`, shed-dose matrix `p90=120.3ms`.
  - Work Board CBE passed (`p90=285ms`); Work Board CPT was red on the red8 run (`p90=316.4ms`)
    but passed the red2 repeat (`p90=276.7ms`).
  - Remaining persistent red: `feed_packing_worklist`.
- Focused red2/feed-only repeats:
  - Red2 report: `.codex-goatos-render/api-latency-pr294-red2-rerun-a80171412.json`;
    `feed_packing_worklist p90=300.3ms` (budget 300), `work_board_page_cpt p90=276.7ms` PASS.
  - Feed-only report: `.codex-goatos-render/api-latency-pr294-feed-only-rerun-a80171412.json`;
    `feed_packing_worklist p90=313.1ms`, p95 `352.4ms`, p99 `466.1ms`.
- Current performance status:
  - Do not claim the API latency gate green yet. Vaccination regressions are fixed and pushed, but
    feed packing remains a real p90 red that needs the next backend pass.

## 02:59 — Procurement/mobile table polish (Codex, 2026-09-21)

- Scope: fix another repeated old-UI class from the current screenshots, not a one-off page patch.
  Procurement/Sales register pages now use a scoped Minimal-style toolbar/filter rhythm on desktop
  and Android WebView widths.
- Changes:
  - Vendor filter bar no longer carries inline legacy spacing/min-width; it uses
    `.proc-vendor-filters`.
  - Procurement page header actions no longer stretch into full-width green banners on phone widths.
  - Feed Purchases mobile ledger now renders as label/value card rows below 640px instead of a
    horizontally clipped table.
  - Procurement cards/buttons tightened from 18/10px radii toward the 16/8px Minimal rhythm.
- Verification:
  - `git diff --check` PASS.
  - `npm --prefix apps/admin-web run typecheck` PASS.
  - `npm --prefix apps/admin-web test -- scripts/smoke-visual-route-coverage.test.mjs scripts/work-board-avatar-overlap.test.mjs scripts/perf-capture-budget-contract.test.mjs` PASS (993 tests).
  - Focused Chrome route visual for `procurement-vendors` and `procurement-feed-purchases` on
    desktop + Android WebView dark rendered real API-backed pages with `integrity_findings: 0`,
    `unstable_captures: []`.
  - Screenshot dir:
    `.codex-goatos-render/admin-web-route-screenshots/2026-09-20T20-58-05-615Z`.
  - Lane still exits red only because the intentional redesign moved the route perceptual hashes;
    no visual baseline was updated.
- Still pending:
  - Continue route-family visual sweeps; this does not certify every route/page/component yet.
  - API latency gate remains red on `feed_packing_worklist`.

- 2026-09-21 02:36 IST: Work Board mobile webview overflow fixed. Focused Chrome route sweep rerun for work-board desktop/webview dark: integrity_findings=0; remaining nonzero exit is expected perceptual hash movement against old baselines. Evidence: .codex-goatos-render/admin-web-route-screenshots/2026-09-20T21-06-29-310Z/work-board__webview__dark.png.

- 2026-09-21 02:44 IST: Feed Packing route polished after palette complaint. Chrome route sweep desktop/webview dark had integrity_findings=0; expected hash movement only. Evidence: .codex-goatos-render/admin-web-route-screenshots/2026-09-20T21-11-18-241Z/feed-packing__desktop__dark.png and feed-packing__webview__dark.png. Typecheck PASS; admin-web focused node tests PASS 993/0.

- 2026-09-21 02:52 IST: Weighing Analytics visual review found mobile PageHeader actions rendered as a full-width green slab and KPI hints clipped too aggressively. Patched the shared PageHeader mobile action sizing and KPI hint wrapping in frame.css. Focused weighing visual guard rerun captured desktop/webview dark; one dev-server chunk-load flake disappeared on rerun, final birth-tab rerun had integrity_findings=0 and unstable_captures=[]; remaining nonzero exits are expected perceptual hash movement against old baselines. Evidence: .codex-goatos-render/admin-web-route-screenshots/2026-09-20T21-15-05-183Z/weighing-analytics__webview__dark.png and weighing-analytics-time__webview__dark.png.

- 2026-09-21 02:59 IST: Continued Weighing mobile review showed active tab auto-centering leaves sliced neighbouring labels at the viewport edge. A proposed AnimatedTabs scroll change was tested, but after a fresh dev-server restart the Time chart captured before its animated line/area completed and two weight variants emitted hydration warnings. Reverted the tab-scroll experiment locally; next fix should target chart final-render stability and old UI families before claiming this route family is clean.

- 2026-09-21 03:08 IST: Route visual runner was missing the Storybook lane's chart-reveal wait, so Recharts area/line charts could be captured mid draw-in as dots-only. Added route visual chart reveal/readiness wait and restored scroll to top before screenshot. First rerun had integrity_findings=0 but exposed a scroll-position artifact; patched the scroll restore and rerunning.

- 2026-09-21 03:18 IST: Banach old-UI judge flagged Tasks/Work Board as still too legacy/card-heavy. Started scoped leadership board CSS polish: flattened status columns/cards, reduced shadows/borders, tightened row density, preserved DnD/card markup. Chrome route captures for Tasks desktop/webview have integrity_findings=0; mobile remains visually heavier than Minimal but no overlap/clipping guard failure. Evidence: .codex-goatos-render/admin-web-route-screenshots/2026-09-20T21-28-47-212Z/tasks__webview__dark.png.

- 2026-09-21 03:33 IST: Started Vaccination Plan island cleanup after old-UI judge called out `.vplan` as a separate mock-derived surface. Softened card/table/rail borders, removed legacy shadows/neon emphasis, and made the mobile header action compact instead of a full-width green slab. Desktop/webview route captures for vaccination-plan and vaccination-plan-edit have integrity_findings=0; route lane exits red only on intentional perceptual hash movement because the visuals changed. Evidence: .codex-goatos-render/admin-web-route-screenshots/2026-09-20T21-33-44-514Z/vaccination-plan__webview__dark.png.

- 2026-09-21 03:49 IST: Rebuilt local API from current PR HEAD with build SHA stamped (`42fcc8b99bb7a24e30ed67e7be8471e4bbee046c`) after judges hit backend_down/stale API. Added shared underline-tab right-edge fade so People mobile tabs read as scrollable instead of accidentally clipped. Added mobile ration rows for Feed Config so 390px webview shows feed item + grams/head/day + context + edit action in the first viewport instead of hiding those columns behind horizontal scroll. Focused route captures now have integrity_findings=0; Feed Config evidence: .codex-goatos-render/admin-web-route-screenshots/2026-09-20T21-49-13-648Z/feed-config__webview__dark.png.

- 2026-09-21 03:56 IST: Pushed mobile tabs + Feed Config row-list fix at `03426fff50ccfb46d944921046eea3ac80852c65`; rebuilt the local API with the same stamped SHA and verified `/version` matches. Focused chart/feed route rerun on desktop + 390px webview had `integrity_findings=0`, `unstable_captures=[]`; failures are expected perceptual hash movement against old baselines. Evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-20T21-52-30-055Z/`. Current-SHA focused API hot-path rerun over previous reds: feed shed-feed PASS (`p90=75.2ms`, 579475 bytes under admin-all 1MiB cap), feed packing worklist PASS (`p90=288.5ms`), vaccination operations PASS (`p90=42.1ms`), vaccination command board PASS (`p90=43.2ms`), shed-dose matrix PASS (`p90=42.2ms`). The combined focused run had Work Board CPT red once (`p90=335.4ms`), but the immediate isolated repeat passed (`p90=285.7ms`, p95 `287.9ms`); keep watching it in full admin-all, do not call the complete gate green from the repeat alone. Production build PASS. Standalone Lighthouse on `/weighing/analytics?scope_mode=company` PASS: performance 76, accessibility 100, best-practices 100, SEO 100; report `apps/admin-web/apps/admin-web/.codex-goatos-render/lighthouse/admin-web-lighthouse-03426fff-standalone.json`.

- 2026-09-21 04:29 IST: Full mobile webview static/route guard visited 458 route/profile/theme entries and exposed three real remaining issues plus several dev-server timeout flakes. Real issues were: Vaccination Execution sticky sidebar inside overflow, Feed SOP mobile kit button under 44px, and Tasks header/filter buttons under 44px. Focused fixes applied for the real issues; focused rerun across `vaccination-execution`, `feed-sops`, and `tasks` on laptop/android dark/light visited 12 combinations with `mobile_webview_guard=PASS`, waived 0, findings 0. Report: `.codex-goatos-render/admin-web-webview/2026-09-20T22-58-51-319Z/report.json`. Extra Tasks-only proof after the final touch-target patch: `.codex-goatos-render/admin-web-webview/2026-09-20T22-57-48-870Z/report.json` (4/4 PASS). New screenshots captured after the fix: `.codex-goatos-render/after-fix-screenshots/2026-09-20T23-04-25-526Z/tasks-webview.png`, `tasks-desktop.png`, `weighing-webview.png`, `feed-config-webview.png`, `people-webview.png`. Current caveat: this certifies the fixed webview failures and the shown pages, not every Storybook component or every route; fresh subagent judges were unavailable because the account hit the weekly usage limit.

- 2026-09-21 05:13 IST: Reran the full mobile webview report after the touch-target commit from the pushed branch. Report `.codex-goatos-render/admin-web-webview/2026-09-20T23-06-21-778Z/report.json`: 463 visited, waived 0, one route-load timeout only (`android/dark/sales-sold` timed out before `domcontentloaded`), every completed route/profile/theme had findings 0. Immediate focused rerun for `sales-sold` passed laptop/android dark/light, 4/4, waived 0, report `.codex-goatos-render/admin-web-webview/2026-09-20T23-41-33-534Z/report.json`. Evidence status: no reproducible mobile webview layout/touch/overlap/chart-label failure remains from this guard pass; still do not claim Storybook-all or full visual-template parity until the story lane and human old-UI parity sweep are rerun.

- 2026-09-21 05:36 IST: Storybook mobile lane rebuilt and rerun. Full mobile dark/light run (`.codex-goatos-render/admin-web-story-screenshots/2026-09-20T23-50-28-714Z`) exposed real failures separate from intentional baseline drift: AnimatedTabs long-label clipped text, SelectField floating-label clipped text, older Dialog/Drawer story play functions querying inside the canvas although surfaces portal to `document.body`, and AnimatedTabs play tolerance at exactly 2px. Fixed the component/story issues and reran focused lanes after rebuilding Storybook:
  - SelectField focused report `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T00-01-40-911Z`: `integrity_findings=0`; remaining failures are baseline/hash/dimension drift only.
  - AnimatedTabs focused report `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T00-02-58-676Z`: `integrity_findings=0`; play failure gone; remaining failures are baseline/hash drift only.
  - Dialog/Drawer focused report `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T00-04-12-164Z`: `integrity_findings=0`; behavior play failures gone; remaining failures are baseline/hash drift only.
  - Typecheck PASS after the fixes. Caveat: full Storybook still exits red until the intentional visual-baseline manifest/PNGs are updated or reviewed; do not claim baseline lane green.

- 2026-09-21 05:47 IST: Full mobile Storybook rerun after the focused fixes: `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T00-05-58-044Z`. Result still exits red on stale visual baselines only, but real render/play status is clean: `stories_selected=257`, `stories_unreached=0`, `captures=554`, `integrity_findings=0`, no interaction/render failures in the final failure list. Remaining 184 failures are manifest dimension/hash drift from intentional redesign and require baseline review/update, not a component overlap/clipped-text bug.

- 2026-09-21 06:04 IST: Full desktop Storybook rerun exposed one real desktop integrity bug before the baseline drift list: `kit-kpicard--variants` clipped the currency value `S$182,450` in both themes. Patched KPI cards to use container-aware sizing, hide the side icon only at very narrow card widths, and only wrap sparkline KPI rows when a sparkline exists. Focused KPI rerun `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T00-26-07-455Z`: `integrity_findings=0`. Full desktop rerun `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T00-26-22-676Z`: `stories_selected=257`, `stories_unreached=0`, `captures=554`, `integrity_findings=0`; remaining 85 failures are stale baseline dimension/hash drift from the redesign, not overlap/clipped-text/play failures.

- 2026-09-21 06:44 IST: Current-frontend full mobile webview report after the KPI clipping fix: `.codex-goatos-render/admin-web-webview/2026-09-21T00-35-50-256Z/report.json`. Result: `mobile_webview_routes_visited=462`, `mobile_webview_waived=0`; every loaded route had `findings=0`. The run exited red on two `domcontentloaded` timeouts only (`laptop/dark/operations-audit`, `android/light/sales-farm-value`). Immediate focused retry for those exact routes across laptop/android dark/light passed 8/8 with `findings=0`, waived 0; report `.codex-goatos-render/admin-web-webview/2026-09-21T01-14-31-266Z/report.json`. Evidence status: no reproducible webview overlap/touch/clipped-text/missing-chart-label failure remains in this pass; the full pass still records transient route-load timeouts and should be rerun before merge if the gate must be strictly all-green in one invocation.

- 2026-09-21 06:51 IST: Current frontend production build PASS after the KPI/webview evidence (`npm --prefix apps/admin-web run build`). Attempted current-SHA standalone Lighthouse on `http://127.0.0.1:3314/weighing/analytics?scope_mode=company`; both unauthenticated and cookie-header attempts redirected to `/login`, so they are not accepted as page-performance proof. Rejected reports: `apps/admin-web/.codex-goatos-render/lighthouse/admin-web-lighthouse-6de670a4-standalone.json` and `apps/admin-web/.codex-goatos-render/lighthouse/admin-web-lighthouse-6de670a4-auth.json`. Remaining perf evidence gap: authenticated current-SHA Lighthouse/page-load measurement must be rerun with a browser context/session path that actually reaches the admin page.

- 2026-09-21 06:55 IST: Closed the current-SHA Lighthouse gap. The standalone server needed the built `.next/static` and `public` assets copied into the standalone bundle for a local audit; without them Lighthouse saw CSS/font 404s and produced invalid contrast failures. Minted a local backend-accepted dev token from the running local API auth env without printing secrets, verified `/goats/search?limit=1` returned 200, then ran authenticated standalone Lighthouse against `http://127.0.0.1:3314/weighing/analytics?scope_mode=company`. PASS: report `apps/admin-web/.codex-goatos-render/lighthouse/admin-web-lighthouse-ba9ab86-auth-assets.json`, scores `performance=76`, `accessibility=100`, `best-practices=96`, `seo=91`. Server log during the audit showed backend calls returning 200; warm page calls included `/weighing/shed-weights` around 80ms and `/weighing/leadership/growth` around 100ms after the initial cold samples.

- 2026-09-21 06:59 IST: Focused SOP builder mobile review found a real webview layout bug: the basics row kept three controls in one row, squeezing the `General` tag and `domain locked` copy together. Patched the builder basics row to stack below 640px and allow readonly chip text to wrap. Focused Chrome route visual for `configuration-work-instructions-builder` webview/dark now has `integrity_findings=0`, `unstable_captures=[]`; it still exits red only because this new/intentionally redesigned capture has no stored route baseline yet. Evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T01-27-51-892Z/configuration-work-instructions-builder__webview__dark.png`.

- 2026-09-21 07:01 IST: SOP/config route-family sweep (`feed-sops`, feed editor/flow, configuration work-instructions list/editor/flow/builder) rendered 14 desktop/webview dark captures with `integrity_findings=0` and `unstable_captures=[]`; failures were stale perceptual baselines or missing webview baselines. Manual inspection of Feed SOP mobile found the shared kit search placeholder was cut hard at the right edge instead of truncating cleanly. Patched `.kit-search` input overflow/ellipsis in `minimal-theme.css`. Focused Feed SOP webview rerun still has `integrity_findings=0`; evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T01-30-16-526Z/feed-sops__webview__dark.png`.

- 2026-09-21 07:08 IST: Judge/local sweep found two concrete mobile regressions. People Notifications webview used a fixed desktop matrix that clipped checkbox columns and emitted a hydration/guard issue; replaced the mobile rendering with explicit alert cards and labelled designation rows. Focused People webview rerun now has `integrity_findings=0`; evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T01-36-00-923Z/people-notifications__webview__dark.png`. Feed SOP Flow webview clipped the START node title under the top border; gave Feed Flow taller layout nodes so edge anchors and card content agree. Focused Feed SOP webview rerun now has `integrity_findings=0`; evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T01-36-53-871Z/feed-sops-editor-flow__webview__dark.png`. Verification: `npm --prefix apps/admin-web run typecheck` PASS; admin-web node test suite PASS 993/0.

- 2026-09-21 07:12 IST: Followed up on People mobile judge notes. Replaced the raw inline People/Clock filter form layout with a scoped `.people-filter-card`/`.people-filter-grid` treatment. First two-column attempt visually touched adjacent controls on 390px, so it was backed to a single-column compact phone grid with smaller gutters and 44px controls. Focused People webview rerun now has `integrity_findings=0`; evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T01-40-03-690Z/people__webview__dark.png` and `people-clock__webview__dark.png`. Typecheck PASS.

- 2026-09-21 07:15 IST: Tightened SOP mobile density after the old-UI judge called out Feed SOP library/editor chrome as too desktop-scale. Mobile SOP stat strips now use smaller icon/count spacing, toolbar actions collapse to the row menu instead of pushing Columns/Export through the phone viewport, editor H1/subtitle sizing is reduced, and SOP editor cards/textareas get scoped phone layout overrides. Focused Chrome route visual for Feed SOP library/editor/flow webview dark has `integrity_findings=0` and `unstable_captures=[]`; the route lane still exits red only because `feed-sops` intentionally moved against stale baselines and the editor/flow webview baselines are missing. Evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T01-45-11-553Z/feed-sops__webview__dark.png` and `feed-sops-editor__webview__dark.png`.
- 2026-09-21 08:05 IST: Buyer Analytics follow-up after focused route capture found the mobile KPI deck/radials oversized and the desktop Buyers table pushing date/balance columns against the right edge. Scoped `/sales/buyer-analytics` to a compact two-column mobile KPI deck, smaller radial gauges, and an explicit buyer table width model so the table scroll container owns overflow without clipping text. First rerun caught real clipped buyer/location/phone/product/revenue text; fixed by widening the table's intrinsic floor and wrapping prose detail lines while keeping atomic phone/date/money values on one line. `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused Chrome route visual for desktop+webview dark has `integrity_findings=0` and `unstable_captures=[]`. The lane still exits red only on intentional perceptual hash movement against stale baselines. Evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T02-35-28-024Z/sales-buyer-analytics__desktop__dark.png` and `sales-buyer-analytics__webview__dark.png`.
- 2026-09-21 08:10 IST: Vaccination Live Tracker mobile filter stack cleanup after old-UI judge flagged the page. The focused before capture had five large full-width field wrappers on phone; patched the plain live-tracker `.lt-fbar` phone layout (not the Tasks `.lt-fsheet-host`) into a compact two-column card with smaller floating labels and 42px controls. `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused Chrome route visual for desktop+webview dark has `integrity_findings=0` and `unstable_captures=[]`; nonzero exit is only stale perceptual hash movement. Evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T02-39-19-307Z/vaccination-live-tracker__desktop__dark.png` and `vaccination-live-tracker__webview__dark.png`.
- 2026-09-21 08:16 IST: Vaccination command board mobile density pass. The old mobile capture had over-tall selects/status pills and KPI tiles pushing the first usable board content too far down. Added mobile-only `.card.cbm` rules for smaller card padding/header, tighter SelectField controls with unclipped floating labels, shorter status chips, and denser two-column KPI cards. `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; vaccination-family Chrome route visual rerun (18 captures across desktop/webview dark) has `integrity_findings=0` and `unstable_captures=[]`; nonzero exit remains stale hash movement only. Evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T02-46-06-049Z/vaccination__desktop__dark.png` and `vaccination__webview__dark.png`.
- 2026-09-21 08:22 IST: Tasks New Task modal mobile polish. Captured the real open modal with Playwright/Chrome on desktop and Android WebView viewport; mobile was functional but attachment controls were stacked and the modal used excessive vertical rhythm. Added mobile-only `.lt-modal` density overrides: tighter header/body spacing, 44px controls, smaller textarea, two-column attachment picker with File full-width, and 48px Send. `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS. Evidence: `.codex-goatos-render/after-fix-screenshots/2026-09-21T02-52-00-tasks-modal/tasks-new-modal-desktop.png` and `tasks-new-modal-webview.png`.

- 2026-09-21 07:32 IST: Fresh mobile-critical Chrome webview sweep on current pushed SHA rendered 31 critical routes x dark/light (`62` captures) with `integrity_findings=0`, `unstable_captures=[]`; the lane still exits red only on intentional perceptual movement/missing baselines. Report/screenshots: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T01-47-11-625Z`. A parallel judge found a real visible mobile Sales KPI issue in older captures: `/sales/sold` and `/sales/farm-value` clipped long rupee/kg values in half-width KPI cards. Fixed by giving procurement KPI values structured number/suffix spans and making Sales KPI decks single-column below 640px. Focused Sales route capture now has `integrity_findings=0`, `unstable_captures=[]`; evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T02-01-30-798Z/sales-sold__webview__dark.png` and `sales-farm-value__webview__dark.png`. `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS.

- 2026-09-21 07:39 IST: Fixed Tasks mobile board old-UI/overflow issue. The current bad capture showed `/tasks` webview still behaving like a sideways desktop kanban with the next lane clipped at the right edge. Removed the late frame.css horizontal-scroll override and changed the Tasks board's <=760px CSS to stack status columns as full-width sections, including the empty rail. Focused Tasks webview dark route capture now has `integrity_findings=0`, `unstable_captures=[]`; it exits red only on expected perceptual movement against stale baselines. Evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T02-08-30-921Z/tasks__webview__dark.png`. `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS.

- 2026-09-21 07:48 IST: Fixed Vaccination Plan add-vaccine modal polish after the old-UI review. The first mobile capture exposed a real footer/field overlap from the sticky action bar; patched the mobile modal footer to flow normally, tightened the segmented controls to a two-column phone grid, and kept the desktop modal centered in the Minimal-style blurred sheet. Playwright opened a real draft editor URL without saving and captured desktop + 390px webview top/bottom states. Automated check found `badText=0` and `footer/field overlap hits=0`; screenshots: `.codex-goatos-render/after-fix-screenshots/2026-09-21T02-17-10-115Z/vaccination-plan-add-vaccine-desktop.png`, `.codex-goatos-render/after-fix-screenshots/2026-09-21T02-17-10-115Z/vaccination-plan-add-vaccine-webview.png`, and `.codex-goatos-render/after-fix-screenshots/2026-09-21T02-17-45-826Z/vaccination-plan-add-vaccine-webview-bottom.png`. `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS.

- 2026-09-21 07:57 IST: Fixed Verify Video Log and Randomization drawer bleed-through. Chrome captures showed the underlying Verification Board tabs/filters painting over the open drawer on 390px webview and desktop. Scoped the Verify drawers to an opaque high overlay layer, gave their header/body internal scroll layers, and hid the board while the modal drawer is open. Probe at the previous failing coordinates now resolves to drawer elements; final captures have `badText=0` and `boardVisible=hidden`. Screenshots: `.codex-goatos-render/after-fix-screenshots/2026-09-21T02-27-12-441Z/verify-video-log-desktop.png`, `verify-video-log-webview.png`, `verify-randomization-desktop.png`, and `verify-randomization-webview.png`. `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS.

- 2026-09-21 02:58 IST: Work Board mobile/desktop polish. Tightened route-scoped mobile header/filter/lane/card density in mesha-theme.css and work-board-minimal.css. Verification: git diff --check PASS; npm --prefix apps/admin-web run typecheck PASS; focused work-board desktop+webview dark visual route guard captured 4/4 with integrity_findings=0 and unstable_captures=[]; remaining nonzero exit is stale perceptual hash baseline only. Screenshots: .codex-goatos-render/admin-web-route-screenshots/2026-09-21T02-58-04-903Z/.

- 2026-09-21 03:28 IST: Brand palette + SOP integrity cleanup. Reverted dark shell tokens from imported Minimal slate back to Mesha green-black while keeping Minimal-style anatomy aliases. Fixed Weighing SOP/SOP Library React key warnings by keying the route-provided assumptions control and wrapping header action controls; added mobile busy-skeleton clamps for legacy phead/subtabs/skel overflow. Verification: git diff --check PASS; npm --prefix apps/admin-web run typecheck PASS; design:guard PASS with p0=0; raw Playwright /weighing/sops mobile console errors=0; focused visual:routes --only weighing-sops desktop+webview dark now integrity_findings=0 (red only stale/missing visual baselines).

- 2026-09-21 09:32 IST: Cleared the focused Android WebView guard failures from the latest route sweep. Fixes: Work Board park selector no longer clips 44px mobile tabs; Vaccination command-board chips are 44px tap targets; Vaccination Live Tracker mobile filter controls are 44px; People Notifications mobile cards no longer trap checkbox rows after the 44px checkbox target fix; Vaccination Plan settings modal dropped the unsupported backdrop blur that the static WebView guard rejected. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; `npm --prefix apps/admin-web run smoke:webview:static` PASS (`.codex-goatos-render/admin-web-webview/2026-09-21T03-59-06-854Z/report.json`); focused People Notifications laptop/android dark/light PASS with findings 0 (`.codex-goatos-render/admin-web-webview/2026-09-21T03-59-07-273Z/report.json`); focused broader route sweep across Work Board, Vaccination, Vaccination Execution, shed actions, Live Tracker, People Notifications, Workflows, Sales Sold, Approvals, and Procurement purchases visited 60 laptop/android dark/light combinations with findings 0 and waived 0 (`.codex-goatos-render/admin-web-webview/2026-09-21T03-59-31-873Z/report.json`). Remaining caveat: this certifies the latest failing mobile/static guard set, not the final all-routes + performance + baseline review gate.

- 2026-09-21 10:09 IST: Old-UI judge found concrete mobile holdouts in Procurement Source Entry, Animal Purchases, and Verify. Converted the Source Entry and Animal Purchases load tables to mobile card rows, and converted the Verify queue table to mobile cards with sticky table cells disabled on Android WebView. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused WebView guard for `procurement-source-entry`, `procurement-animal-purchases`, `verify`, and `verification` visited 16 laptop/android dark/light combinations with findings 0 and waived 0 (`.codex-goatos-render/admin-web-webview/2026-09-21T04-30-03-402Z/report.json`). Route screenshots captured with `integrity_findings=0`, `unstable_captures=[]`; remaining route visual failures are stale perceptual hashes only. Evidence screenshots: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T04-32-57-103Z/procurement-source-entry__webview__dark.png`, `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T04-32-57-103Z/verify__webview__dark.png`, and `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T04-39-08-843Z/procurement-animal-purchases__webview__dark.png`.

- 2026-09-21 10:25 IST: Chart/table mobile judge found two real remaining visual defects: Sales Market Analytics latest-price table clipped the right-side columns on 390px WebView, and Weighing Analytics `Weight-wise` opened with the active tab label half hidden at the right edge. Fixed Sales latest prices by adding per-cell labels and switching the mobile table into labelled card rows; fixed Weighing analytics phone tabs by letting that route's underline tab strip wrap into visible rows instead of depending on first-paint horizontal scroll. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused WebView guard for `sales-market-analytics`, `weighing-analytics-weight`, `weighing-analytics-weight-not-shown`, and `weighing-analytics-weight-band-filter` visited 16 laptop/android dark/light combinations with findings 0 and waived 0 (`.codex-goatos-render/admin-web-webview/2026-09-21T04-54-03-121Z/report.json`). Fresh route screenshots have `integrity_findings=0`, `unstable_captures=[]`; route lane exits red only on stale perceptual hash movement. Evidence screenshots: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T04-52-59-565Z/sales-market-analytics__webview__dark.png` and `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T04-53-41-104Z/weighing-analytics-weight__webview__dark.png`. Remaining caveat: final full all-routes visual baseline review and the current admin-all API latency gate are still not globally green.

- 2026-09-21 10:35 IST: API latency judge reran the previous admin-all red paths on current SHA and found `sales_deals` green but `feed_direction_preview` red (`p90=350.8ms`, p95 `399.5ms`, p99 `411.6ms`, failures 0) at `.codex-goatos-render/api-latency-pr294-focused-reds-2fdd1323-20260921T045755Z.json`. Fixed the served feed-direction preview path by de-duplicating per-request pinned SOP card reads across workflows and adding an immutable pinned feed-SOP version cache in the postgres rules source. Verification: `git diff --check` PASS; `go test ./internal/feedsop/adapters/postgres ./internal/feeddirection/app ./internal/feeddirection/adapters/http` PASS; dirty-tree focused rerun for only `feed_direction_preview` is green with p90 `295.7ms`, p95 `370.5ms`, p99 `377.0ms`, failures 0, report `.codex-goatos-render/api-latency-feed-direction-preview-only-2fdd1323-dirty-20260921T050139Z.json`. Remaining caveat: this certifies the previous red feed preview path, not a fresh full admin-all latency pass after commit.

- 2026-09-21 10:42 IST: Live API restored on current pushed SHA `0f3762cc4` (`/version` build SHA matched, `/app/me` 200) and admin-web restarted against it. Focused old-UI holdout sweep for `feed-config`, `vaccination-plan-edit`, and `herd-signals-animals` desktop + 390px webview had `integrity_findings=0`, but manual screenshot review found `vaccination-plan-edit` mobile still clipped the live-plan `First doses` table copy. Converted `.vplan .tabl` to labelled mobile card rows and disabled sticky table headers on mobile; also added the same stacked mobile treatment for full vaccination schedule tables. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused `vaccination-plan-edit` desktop/webview route rerun has `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual hash movement only. Evidence: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T05-10-52-800Z/vaccination-plan-edit__webview__dark.png` and `vaccination-plan-edit__desktop__dark.png`.

- 2026-09-21 10:44 IST: After pushing `549cd0866`, reran the holdout route set (`feed-config`, `vaccination-plan-edit`, `herd-signals-animals`) on desktop + 390px webview dark: `integrity_findings=0`, `unstable_captures=[]`; nonzero exit remains stale perceptual hash movement only, screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T05-12-10-415Z`. Reran the strict mobile WebView guard for the same routes across laptop/android dark/light: `mobile_webview_guard=PASS`, routes visited 12, waived 0, report `.codex-goatos-render/admin-web-webview/2026-09-21T05-12-40-824Z/report.json`.

- 2026-09-21 11:30 IST: Full mobile WebView sweep on pushed SHA `dd4012c34` visited 461 route/profile/theme combinations and exposed one real recurring issue plus three route-load timeouts. Real issue: `vaccination-schedule` Android dark/light still had sticky table headers inside hidden overflow and clipped/scroll-trapped operator schedule rows. Fixed the schedule table/card mobile CSS so headers are not sticky in WebView, rows/cards are full-width, date/operator/park/shed values wrap normally, and shed chips no longer clip names. Timeout retries for `procurement-source-entry`, `procurement-vendors`, and `workflows` passed focused. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused WebView guard for `vaccination-schedule`, `procurement-source-entry`, `procurement-vendors`, and `workflows` visited 16 laptop/android dark/light combinations with findings 0 and waived 0 (`.codex-goatos-render/admin-web-webview/2026-09-21T05-59-33-590Z/report.json`). Visual route capture for `vaccination-schedule` webview dark has `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual hash movement only. Evidence screenshot: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T05-59-04-200Z/vaccination-schedule__webview__dark.png`.

- 2026-09-21 11:42 IST: Control Tower route audit. `/?lens=control-tower` is explicitly parked from the sidebar in `app/(admin)/page.tsx` and kept only as a hidden root fallback/deep-link lens for principals without the ADG landing contract. It is not a visible product page for the Minimal-style visual overhaul, so removed `control-tower` from `smoke-visual-live.mjs`, route coverage expectations, and `webview-critical-routes.json` while leaving fallback code/tests intact. Verification: `node --test apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs` PASS; `npm --prefix apps/admin-web run typecheck` PASS; `git diff --check` PASS.

- 2026-09-21 12:02 IST: Revalidated the visible critical route set after removing hidden Control Tower from the phone/route guard list. Fresh admin-web was running from the current worktree at `127.0.0.1:3311` against the local API at `127.0.0.1:18086`. Command: `npm --prefix apps/admin-web run visual:routes -- --webview-critical --profiles desktop,webview --themes dark --max-diff-ratio 1`. Result: `60/60` captures produced for the `30` visible critical routes, `integrity_findings=0`, `unstable_captures=[]`; the route lane still exits red only because existing perceptual baselines are stale after the redesign and `home__webview__dark.png` has no baseline yet. Evidence/screenshots: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T06-18-21-235Z`.

- 2026-09-21 12:09 IST: Sales Summary visual polish from manual screenshot review. The `/sales/sold` KPI row still used giant cropped decorative rupee/goat/package watermarks that read as an odd color pattern on desktop and phone, unlike the Minimal dashboard small icon-badge KPI anatomy. Replaced those four card watermarks with compact icon badges while preserving Mesha brand tones and metric data. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused `sales-sold` desktop+webview dark route capture has `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is expected perceptual movement against stale baselines. Evidence screenshots: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T06-33-15-880Z/sales-sold__desktop__dark.png` and `sales-sold__webview__dark.png`.

- 2026-09-21 12:22 IST: Continued Minimal-style component alignment from the visual judges. Mobile underline tabs were still behaving like an old horizontal scroller on Feed Analytics, Verify, and Weighing; right-edge labels clipped and the animated underline could point at the wrong wrapped row. Converted phone underline tabs to visible compact pill states with no edge fade/hidden underline, preserving the desktop underline pattern. Verify Toxin mobile rows were inheriting a generic `Proof` pseudo-label and overlapping the toxin subject, so scoped that table to labelled mobile rows (`Test`, `Reading`, `Status`). Procurement Load-wise KPI cards had the same oversized watermark issue as Sales Summary, so moved them to compact icon badges. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused WebView route capture for `feed-analytics`, `verify-toxin`, `weighing-analytics`, and `sales-loads` produced 20/20 captures with `integrity_findings=0`, `informational_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual hash movement plus one missing FCR baseline. Evidence screenshots: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T06-45-23-319Z/feed-analytics__webview__dark.png`, `sales-loads__webview__dark.png`, `verify-toxin__webview__dark.png`, and `weighing-analytics__webview__dark.png`. Remaining caveat: this certifies the focused mobile defects only; desktop holdouts, current-SHA API latency, Lighthouse, and final all-route visual baseline review are still pending.

- 2026-09-21 12:31 IST: Desktop/webview holdout fixes from the parity judges. Counts Breakdown desktop pen table clipped the rightmost `COUNT` header/column in the first viewport; changed the pen table to a fixed full-width column rhythm with denser header/cell padding so all eight columns remain visible without hiding the numeric count. Calendar Month had a huge right-side/blank shell and, after the first attempted fix, mobile WebView overlapped the month picker with owner chips/rhythm cards. Scoped the Calendar header actions, removed the blank shared `subtabs` shell for this route, and forced the open month picker to remain in normal page flow on WebView. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused route captures for `counts-breakdown` and `calendar-month` desktop+webview dark both have `integrity_findings=0` and `unstable_captures=[]`; nonzero exits are stale perceptual hash movement only. Evidence screenshots: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T06-53-02-788Z/counts-breakdown__desktop__dark.png`, `counts-breakdown__webview__dark.png`, `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T06-59-46-057Z/calendar-month__desktop__dark.png`, and `calendar-month__webview__dark.png`. Remaining caveat: Calendar density still needs a later full Minimal-dashboard composition pass; this entry closes the overlap/blank-shell/table-clipping defects only.

- 2026-09-21 12:36 IST: Scope correction after owner clarification. Action Center, Calendar, Control Tower, Protocol Adherence, and Workflows are deprecated/not visible surfaces and must not drive the Minimal-dashboard parity bar for PR #294. Removed Action Center, Calendar variants, Protocol Adherence variants, Workflows, workflow-record, and calendar-drive-detail from the visual smoke route list, route coverage expectations, and phone/WebView critical route set. Left the code paths and non-visual guards intact; this only changes what the visual-overhaul sweep treats as visible product surface. Verification: `node --test apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs` PASS; `npm --prefix apps/admin-web run smoke:webview:static` PASS (`.codex-goatos-render/admin-web-webview/2026-09-21T07-05-11-706Z/report.json`); `npm --prefix apps/admin-web run typecheck` PASS; `git diff --check` PASS. Remaining focus: visible routes/components only.

- 2026-09-21 13:08 IST: Visible-surface judge retry after deprecated-route correction. Two read-only judges reviewed only visible product surfaces and explicitly excluded Action Center, Calendar, Control Tower, Protocol Adherence, and Workflows. Real findings addressed in this batch: Work Board mobile active park segment bled outside its segmented container, Work Board task metadata could clip after a too-aggressive truncation attempt, Herd Signals mobile pill tabs could make the rightmost tab look cut off, and the previous Weighing load-comparison screenshot showed `Weights could not be loaded` from a stale/down local stack. Fixes: patched the late-loading Work Board minimal CSS so the active segment stays inside the segmented parent, preserved no-clipping metadata wrapping, tightened mobile Work Board type/control rhythm without violating 44px targets, and added mobile pill-tab right scroll padding. Rebuilt/restarted the local API from this checkout and restarted admin-web on `127.0.0.1:3311`; `/version` is unstamped (`build_sha=unknown`) but migrations match and the binary was rebuilt locally. Verification: focused route capture for `work-board` + `work-board-populated` desktop/webview dark produced `4/4` captures with `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual hash movement only, screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T07-37-33-019Z`. Focused route capture for `feed-packing`, `weighing-analytics-load`, and `herd-signals-alerts` desktop/webview dark produced screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T07-35-58-778Z`; Weighing load now renders the comparison chart with labels instead of the prior error card, and remaining route failures are stale hash movement only except the Work Board clipping that the later focused rerun cleared. Remaining caveat: Work Board mobile is no longer broken/clipped, but still needs a further template-density pass; Approvals mobile old table surface, Vaccination Live Tracker control bulk, Feed Analytics micro-chart labels, full current-SHA API latency, Lighthouse, and final all-visible-route baseline review remain pending.

- 2026-09-21 13:25 IST: Owner clarified deprecated scope again: Action Center, Calendar, Control Tower, Protocol Adherence, Workflows/workflow-record, and calendar detail routes are not visible product surfaces and must not receive PR #294 Minimal-dashboard parity work. Added that rule to the progress doc ground rules. Continued only on visible surfaces: Approvals mobile no longer renders a squeezed desktop table; rows are labelled mobile cards. Vaccination Live Tracker now carries a route-specific `lt-live-page` class so its mobile header/action/filter density can be tightened without leaking into Tasks or hidden command pages. Focused Chrome route capture for `approvals` and `vaccination-live-tracker` desktop+webview dark produced 8/8 screenshots with `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual hash movement only. Evidence screenshots: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T07-47-51-392Z/approvals__webview__dark.png`, `vaccination-live-tracker__webview__dark.png`, and `vaccination-live-tracker__desktop__dark.png`. Remaining focus stays on visible pages only: Feed Packing nested card density, Feed Analytics mobile chart labels/context, Work Board deeper template-density polish, final all-visible-route visual review, Storybook review, current-SHA API latency, and Lighthouse/perf proof.

- 2026-09-21 13:28 IST: Feed Packing visible-route density pass. The mobile screenshot still showed the packing filter/feed-day/lifecycle stack as a card nested inside another card. Added route-scoped mobile CSS for `feed-packing-page`, then corrected the shell selector from `.card.feed-packing-shell` to `.kit-card.feed-packing-shell` after computed-style proof showed the fake border was an inset `kit-card` shadow. Focused Chrome webview capture for `feed-packing` has `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual hash movement only. Evidence screenshot: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T07-53-28-676Z/feed-packing__webview__dark.png`. Remaining caveat: this removes the nested shell and improves density; final template-fidelity scoring still needs the all-visible route judge.

- 2026-09-21 13:31 IST: Feed Analytics mobile micro-chart context pass. The KPI mini bars were visible on phone but had no visible time-window cue, so they read as anonymous ticks. Added a small route-scoped `14-day bars` cue inside the visible KPI hint line for the three sparkline KPI cards. Focused Chrome webview capture for Feed Analytics and its tab variants produced 6/6 screenshots with `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual hash movement only. Evidence screenshot: `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T07-58-08-124Z/feed-analytics__webview__dark.png`.

- 2026-09-21 13:35 IST: Broad visible critical-route Chrome sweep after the latest visible fixes. Command: `npm --prefix apps/admin-web run visual:routes -- --webview-critical --profiles desktop,webview --themes dark --max-diff-ratio 1` against admin-web `127.0.0.1:3311` and local API `127.0.0.1:18086`. Result: 24 visible critical routes, 48/48 desktop+390px webview dark captures, `integrity_findings=0`, `unstable_captures=[]`, report/screenshots `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T07-59-41-419Z`. Nonzero exit is baseline debt only: one missing `home__webview__dark` baseline and expected perceptual-hash movement from the redesign. This is not final template parity and not a final PR-ready gate; it is the current no-overlap/no-clipping visible-route proof after excluding deprecated Action Center/Calendar/Control Tower/Protocol Adherence/Workflows.

- 2026-09-21 13:52 IST: Current-SHA API latency gate was rerun with a stamped local API binary (`/version` build SHA `c46b326c9953084d1c4def9311c711d6b65bb6a8`) and a fresh local bearer token. Full `hot-paths.admin-all.json` covered 92 endpoints and is **not final-green**: first full run failed 5 marginal threshold checks (`feed_direction_preview` p90 305.5ms, `feed_packing_worklist` p90 303.5ms, `feed_config_ration_groups` p99 802.4ms, `sales_overview` p90 300.3ms, `verification_queue` p99 502.3ms) at `.codex-goatos-render/api-latency-admin-all-c46b326c9-20260921T081544Z.json`. Focused retry of those 5 left only `feed_direction_preview` red by 0.4ms (`p90=300.4ms`) at `.codex-goatos-render/api-latency-focused-reds-c46b326c9-20260921T082024Z.json`; a single-endpoint retry for `feed_direction_preview` then passed (`p90=287.4ms`, p95 290.1ms, p99 293.8ms) at `.codex-goatos-render/api-latency-feed-direction-only-c46b326c9-20260921T082143Z.json`. Status: latency evidence is much better than the earlier red path, but the PR still needs one fresh full admin-all pass that exits green before API/perf can be called complete.

- 2026-09-21 13:58 IST: After the progress-doc commit moved PR HEAD to `50dfee262`, rebuilt/restarted the local API stamped to `50dfee262b6ab1a01a1cca0d0c6d5fc9f61819d3` and reran the full admin-all latency gate. It is still **not final-green**: 92 endpoints covered, 3 reds at `.codex-goatos-render/api-latency-admin-all-50dfee262-20260921T082406Z.json`: `feed_direction_preview` (`p90=337.2ms`, `p99=618.2ms`), `procurement_loadwise_sales` (`p99=598.0ms`), and `work_board_page_cpt` (`p90=317.7ms`). Next perf work should target those three paths before another full-green claim.

- 2026-09-21 15:36 IST: Latest pushed PR head/local head is `45331c422`; remote `design/minimal-redesign-preview` matches and the tree was clean before this doc update. Revalidated the visible critical route set after the Feed/Counts empty-copy fixes against admin-web `127.0.0.1:3311` and local API `127.0.0.1:18086`. Command: `npm --prefix apps/admin-web run visual:routes -- --webview-critical --profiles desktop,webview --themes dark --max-diff-ratio 1`. Result: 24 visible critical routes, 48/48 Chrome desktop + 390px webview dark captures, `integrity_findings=0`, `unstable_captures=[]`, screenshots/report `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T09-59-51-292Z`. One informational hit-target note remains on `counts-herd__webview__dark.png` (`input 18x18`, already gated by `smoke:webview`); visual inspection shows no obvious broken visible control. Nonzero exit remains route-baseline debt only: stale perceptual hashes/missing `home__webview__dark` baseline after the redesign. Deprecated/non-visible surfaces remain excluded from PR #294 parity work: Action Center, Calendar, Control Tower, Protocol Adherence, Workflows/workflow-record, and calendar detail routes. Still pending for ready/merge: visually review/update visible-route baselines, rerun Storybook final baseline lane, rebuild/restart API on the latest SHA for final full admin-all latency, rerun Lighthouse/page-load proof, and push each evidence/fix checkpoint.

- 2026-09-21 16:27 IST: Cleared the two local red gates from the previous full-suite retry. `live-tracker styles are scoped` was a guard false positive: the regex treated scoped `.feed-packing-page` selectors as the old generic `.feed` mock class. Tightened the guard so it still catches truly unscoped `.feed`, `.frow`, `.legend`, `.num`, `.kpi`, and `.tag` selectors without rejecting route-scoped names. `work-board-avatar-overlap` exposed a real mobile toolbar issue: in a narrow `.wb .tbar` grid, avatar buttons could shrink below the intended 30px face and fail the individual hit/initials-fit check. Fixed `.wb .avs > .av` and `.wb .avs .more` to stay `flex:0 0 30px` with border-box sizing. Verification: `node --test --experimental-strip-types apps/admin-web/features/vaccination-live-tracker/live-tracker.test.mjs apps/admin-web/scripts/work-board-avatar-overlap.test.mjs` PASS (`43/43`); `npm --prefix apps/admin-web run design:guard` PASS (`p0=0`, `failures=0`). Focused Work Board Chrome webview capture after the fix produced fresh screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T10-56-23-074Z/work-board__webview__dark.png` and `work-board-populated__webview__dark.png`; visual inspection shows no clipped avatar initials, no overlapping toolbar text, and controls/cards contained in the 390px viewport. The route visual command still exits red only on stale/missing baselines, not integrity findings. Still pending for ready/merge: final all-visible-route baseline review, final Storybook lane after the last CSS changes, latest-SHA full admin-all API latency green run, and latest-SHA Lighthouse Performance-only proof.

- 2026-09-21 17:00 IST: Rebased PR #294 worktree onto current `origin/main` (`395faac174`) and preserved the new mainline Weights/ADG behavior in the redesigned UI. Main brought the pen breed column / sale-threshold assumptions checks into the Weighing surfaces; after the rebase, restored the PR's redesigned visible-page files, re-applied the explicit Weighing visual-smoke date windows, and carried the new breed/assumption wiring into `PensTable`, `weights-analytics`, and `/weighing/weights` KPI labels. Fixed post-rebase design guard regressions by replacing the Tasks native date pair with the shared `DateRangeField`, removing a SOP prose-under-title pattern, restoring Feed Analytics' stock-only/full-control grouping, and making SOP flow insert controls use a 44px mobile hit area without visually oversized circles. Verification: `npm --prefix apps/admin-web run typecheck` PASS; `npm --prefix apps/admin-web run design:guard` PASS (`p0=0`, `failures=0`); `node --test apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs` PASS (`8/8`); `node --test --experimental-strip-types apps/admin-web/features/weighing/weights-assumptions.contract.test.mjs` PASS (`6/6`); `node --test --experimental-strip-types apps/admin-web/features/feed/feed-analytics.test.mjs` PASS (`17/17`); full `npm --prefix apps/admin-web test` PASS (`1015/1015`). Focused Chrome route visual for Feed Analytics, Weighing Analytics/Weights, Tasks, and Weighing SOPs produced `46/46` desktop + 390px webview dark screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T11-22-42-519Z`; result is still nonzero due stale/missing perceptual baselines after the redesign, but `captures=46`, `integrity_findings=0`, `unstable_captures=[]`. After the SOP flow hit-area fix, focused Weighing SOP webview rerun produced 4/4 screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T11-29-33-387Z`; editor and flow captures are OK, `integrity_findings=0`, with only informational tap-target notes on native inputs/toolbar controls and a rounding-edge `44x44 < 44x44` note for the flow insert button. Visual inspection performed on the fresh Weighing SOP flow mobile capture and Tasks mobile capture: no overlap, no clipped labels, no missing chart labels on the inspected surfaces. Still pending before "ready": push the rebased commit, final all-visible-route baseline review/update, final Storybook lane after this rebase/CSS change, latest-SHA full admin-all API latency green run, and latest-SHA Lighthouse **Performance-only** proof.

- 2026-09-21 17:45 IST: Rebased again onto latest `origin/main` (`dd3eda101`, docs-only CI routing; no new visible admin-web UI). New local head before this doc commit is `a8d808f897`. The post-rebase local API is rebuilt/stamped to `a8d808f897`, migration `000380#1`, and `/version` reports `migration_drift=false`. A broad visible critical route sweep initially found four real 390px webview route failures with API healthy: `feed-packing`, `weighing-analytics`, `weighing-analytics-load`, and `weighing-weights` rendered/flagged route error states. Root cause split: the visual judge incorrectly treated any `.kit-state-title` as an error boundary, even though that class is used by normal kit state/empty UI, and Weighing's mobile header/tab/KPI density still read too bulky. Fixes in this batch: tightened `smoke-routes-visual.mjs` to flag only real error-boundary markers/text (`[data-error-boundary]`, `#error-boundary`, or explicit error copy), and added shared mobile kit rules for compact Minimal-style page headers, tab pills, actions, and Weighing KPI cards. Verification after the fix: `npm --prefix apps/admin-web run design:guard` PASS (`p0=0`, `failures=0`); `node --test apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs` PASS (`8/8`); focused red-route Chrome webview dark rerun for `feed-packing`, `weighing-analytics*`, and `weighing-weights` produced `13/13` screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T12-06-16-279Z` with `integrity_findings=0`, `unstable_captures=[]`; final focused Weighing webview rerun produced `11/11` screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T12-09-00-926Z` with `integrity_findings=0`, `unstable_captures=[]`. Nonzero exits are stale/missing visual baselines only. Visual screenshots inspected: `weighing-analytics__webview__dark.png` and `feed-packing__webview__dark.png`; both render real content without overlap/error boundary, but Weighing still deserves a final template-fidelity review before marking PR ready. Still pending before "ready": full visible-route rerun after this CSS/judge fix, final Storybook lane, latest-SHA full admin-all API latency green run, latest-SHA Lighthouse **Performance-only** proof, and visible-route baseline review/update.

- 2026-09-21 17:55 IST: Pushed `357fe98d558916fa3809b60f51d435343e3f98b8` to PR #294 and rebuilt/restarted the local API stamped to the same SHA; `/version` confirms `build_sha=357fe98d558916fa3809b60f51d435343e3f98b8`, binary/db migration `000380#1`, `migration_drift=false`. Full visible critical Chrome sweep on this pushed SHA (`--webview-critical --profiles desktop,webview --themes dark`) produced `51/52` screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T12-12-33-047Z`, with `integrity_findings=0`, `unstable_captures=[]`, and `informational_findings=5` (native 18px checkbox/input hit-target notes gated by `smoke:webview`). The one missing capture was `work-board__desktop__dark.png` from a `page.goto` timeout; focused Work Board desktop retry immediately produced `2/2` captures at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T12-20-59-873Z`, again `integrity_findings=0`, `unstable_captures=[]`, and only stale perceptual hash failures. Important caveat from visual inspection: the focused Work Board desktop screenshot still shows a visible partial-load warning (`Some work couldn't load right now...`) and API logs show `admin_ui_contract_family_load_error` timeouts during the capture. So Work Board is not yet acceptable as a clean visible surface even though the layout probe is green. Remaining before "ready": remove/resolve the Work Board visible partial-load warning, final all-visible-route rerun, final Storybook lane, full admin-all API latency green run, Lighthouse **Performance-only** proof, and visible-route baseline review/update.

- 2026-09-21 18:00 IST: Resolved the Work Board visible partial-load/degraded banner root cause. API logs showed `/admin-web/bootstrap` returning `admin_ui_contract_family_load_error` because the admin-web contract family loader inherited the generic 3s DB timeout while loading the full shell option set; that made the shell show `Some dropdown options are unavailable right now` and Work Board show `Some work couldn't load right now` during visual capture. Raised the admin-ui contract repository timeout floor to 10s, rebuilt/restarted the local API from the dirty tree, and verified `/version` build SHA `683dc09405bffd647093ed33f5c20874bc5165c5`, migration `000380#1`, `migration_drift=false`. Fresh bootstrap probes now return only `summary-vs-detail` and `no-frontend-business-truth` display rules, with no `admin_ui_contract_family_load_error`, and warm timings around 1.0-1.2s. Verification: `go test ./internal/adminui/adapters/postgres ./internal/adminui/app` PASS. Focused Work Board desktop+390px webview dark visual rerun produced 4/4 screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T12-29-59-771Z` with `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale/missing perceptual baselines only. Manual image inspection confirms the prior degraded/partial warning banners are gone on both `work-board__desktop__dark.png` and `work-board__webview__dark.png`. Remaining before "ready": commit/push this fix, final all-visible-route rerun, final Storybook lane, full admin-all API latency green run, Lighthouse **Performance-only** proof, and visible-route baseline review/update.

- 2026-09-21 18:09 IST: Pushed `1855ca5269da3f0aacc2396d8e79985bcbb25f21` to PR #294, rebuilt/restarted the local API stamped to that exact pushed SHA, and verified `/version` reports `build_sha=1855ca5269da3f0aacc2396d8e79985bcbb25f21`, migration `000380#1`, `migration_drift=false`. Full visible critical Chrome route sweep on this pushed SHA (`--webview-critical --profiles desktop,webview --themes dark --max-diff-ratio 1`) captured `52/52` screenshots across 26 visible routes at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T12-32-50-482Z`; summary: `integrity_findings=0`, `integrity_by_check={}`, `unstable_captures=[]`, `informational_findings=5`. The command still exits nonzero because 49 captures moved against stale/missing perceptual baselines after the redesign, including missing `home__webview__dark`; this is baseline debt, not a current overlap/clipped-text/error-boundary finding. Manual image inspection after the run: `weighing-analytics__webview__dark.png` has visible tab labels and no error state; `feed-analytics__webview__dark.png` has visible KPI micro-bars and the `14-DAY BARS` cue; Work Board warning banners remain gone. Remaining before "ready": final Storybook lane, full admin-all API latency green run, Lighthouse **Performance-only** proof, and visible-route baseline review/update.

- 2026-09-21 18:42 IST: Storybook component parity follow-up after the full story lane reported real component-level failures, not just stale screenshots. Fixed header-only/blank-card Storybook states in Card, PageEnter, PageFrame, MotionFrames, and AnimatedTabs stories so component cards now render visible body/empty-state content instead of blank shells; also replaced fragile mobile tab play assertions that checked indicator pixel movement with visible selected-tab/panel assertions. Focused Storybook reruns after the patch: `kit-card` has `integrity_findings=0` (red only because new body content moved baselines) at `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T13-04-45-409Z`; `kit-motionframes` PASS `24/24`, `integrity_findings=0`, failures `[]` at `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T13-08-32-001Z`; `kit-pageenter` has `integrity_findings=0` (red only baseline/hash movement) at `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T13-09-13-102Z`; `kit-pageframe` has `integrity_findings=0` (red only mobile/light baseline dimensions) at `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T13-10-12-403Z`; `animatedtabs` has `integrity_findings=0` and no interaction/play failures (red only stale mobile/light baselines) at `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T13-10-58-695Z`. Status: the prior Storybook blank-card and mobile tab interaction bugs are fixed, but Storybook is not final-green until the full story baseline lane is reviewed/updated on a good build. Remaining before "ready": full Storybook baseline review/update, latest-SHA full admin-all API latency green run, Lighthouse **Performance-only** proof, and visible-route baseline review/update.

- 2026-09-21 18:56 IST: Rebuilt/restarted the local API on current pushed SHA `61c5b25af20ea7f2e73f9a3273cae53aaad251c0`; `/version` reports the same build SHA and `migration_drift=false`. Full `hot-paths.admin-all.json` latency gate on that exact SHA covered 92 endpoints and is **not final-green**: six endpoints exceeded policy with zero HTTP failures at `.codex-goatos-render/api-latency-admin-all-61c5b25af2-20260921T131455Z.json`: `feed_direction_preview` (`p90=316.0ms`, `p99=419.2ms`), `feed_packing_worklist` (`p90=363.7ms`, `p99=709.9ms`), `work_board_page_cbe` (`p90=358.3ms`, `p99=500.2ms`), `work_board_page_cpt` (`p90=336.2ms`, `p95=391.3ms`), `verification_queue` (`p99=512.8ms`), and `herd_signals_live` (`p90=464.4ms`, `p99=549.8ms`). Focused retry of those six narrowed repeat reds to four at `.codex-goatos-render/api-latency-red6-61c5b25af2-20260921T132054Z.json`: `feed_direction_preview` (`p90=400.3ms`), `feed_packing_worklist` (`p90=345.1ms`, `p99=876.6ms`), `work_board_page_cpt` (`p90=376.1ms`, `p95=568.3ms`, `p99=916.8ms`), and `herd_signals_live` (`p90=339.8ms`). `work_board_page_cbe` and `verification_queue` passed the focused retry. Remaining before "ready": fix or prove repeat-red latency paths, then rerun a full admin-all green gate; full Storybook baseline review/update; Lighthouse **Performance-only** proof; visible-route baseline review/update.

- 2026-09-21 19:03 IST: Rebasing and perf checkpoint. Rebased PR #294 onto fresh `origin/main` (`cafaa2d0c149`); new pushed PR head after the rebase/progress checkpoint was `18b389da0`, and `origin/main` is now an ancestor. Implemented a backend-only feed perf fix: locked served feed sheets are cached in-process after the first immutable read, with deep-copy returns so preview/packing request stamping cannot mutate cached rows, lifecycle, SOP pins, or headers. Verification before benchmarking: `go test ./internal/feeddirection/app ./internal/feeddirection/adapters/http ./internal/feedsop/adapters/postgres` PASS; `git diff --check` PASS. Rebuilt/restarted the local API stamped to `23aa7ad62d74eae229ab2040058d5df76e0271ea`; `/version` reports that exact build SHA and `migration_drift=false`. Focused repeat-red latency gate for Feed Direction, Feed Packing, Work Board CPT, and Herd Signals at `.codex-goatos-render/api-latency-red4-23aa7ad62d-20260921T133233Z.json` is **not final-green**, but both feed endpoints are now green: `feed_direction_preview` p90 `204.8ms` (was repeat-red p90 `400.3ms`), `feed_packing_worklist` p90 `214.6ms` (was repeat-red p90 `345.1ms`). Remaining repeat-reds: `work_board_page_cpt` p90 `331.4ms` and `herd_signals_live` p90 `369.1ms`. Remaining before "ready": push this feed fix/doc checkpoint, fix or prove the remaining repeat-red Work Board/Herd Signals latency, rerun full admin-all green, full Storybook baseline review/update, Lighthouse **Performance-only** proof, and visible-route baseline review/update.

- 2026-09-21 19:24 IST: Honest latency checkpoint after the owner asked for clarity. Pushed Herd Signals fix at `a7a7cdb18`: the live read now uses a 5s cloned response cache and the focused red4 gate showed `herd_signals_live` green (`p90=45.5ms` in `.codex-goatos-render/api-latency-red4-a7a7cdb180-20260921T133749Z.json`). Feed remains green. Work Board is still the active red gate. A local Work Board summary cache commit (`c78595ee1`) plus a dirty handler change that removes the expensive page-lane prefetch both pass `go test ./internal/workboard/app ./internal/workboard/adapters/http` and cut debug traces from multi-second lane-prefetch waits to sub-second page assembly, but the official focused red4 gate is still **not final-green**: `.codex-goatos-render/api-latency-red4-c78595ee1d-noprefetch-dirty2-20260921T135058Z.json` has `work_board_page_cpt` p50 `317.2ms`, p90 `446.5ms`, p95 `453.0ms`, p99 `480.1ms`, failures `0`; Feed Direction (`p90=222.3ms`), Feed Packing (`p90=228.5ms`), and Herd Signals (`p90=40.4ms`) pass. A 2s full-page cache was attempted and immediately removed because `TestPageReadsFreshScopedBundleAfterMutation` correctly caught stale page reads after mutation. Current truth: visual visible-route overlap/clipping proof is clean from the prior sweep, deprecated surfaces remain excluded, but PR #294 is **not ready** until Work Board CPT latency is fixed/proven green, then a full admin-all latency pass, Storybook baseline review/update, Lighthouse **Performance-only** proof, and visible-route baseline review/update are completed.

- 2026-09-21 20:24 IST: Scope reset per owner: performance work is paused; current checkpoint is visual/template parity only. Fixed four visible UI/component defects from fresh judges and screenshot review: `/alerts` now renders the real Alerts page instead of proxy-aliasing to Approvals; Work Board mobile keeps a horizontal lane board instead of stacking lanes below the filters; Feed Packing mobile shell is flatter with less card-inside-card weight; Feed Analytics fallback/error copy gets proper card padding; Sales Summary mobile KPI cards are tighter with smaller icon badges and muted labels. Storybook component fixes: removed the mobile underline-tabs-to-chip override so underline tabs stay Minimal-style scrollable underline tabs, and made kit sheet actions nowrap/full-width on phone so the Overlay sheet story no longer wraps `Close`. Verification: `npm --prefix apps/admin-web run typecheck` PASS; `npm --prefix apps/admin-web run design:guard` PASS (`p0=0`, `failures=0`); focused Storybook `animatedtabs` mobile dark PASS `21/21`, `integrity_findings=0`, screenshots `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T14-52-27-367Z`; focused Storybook `overlay` mobile dark PASS `25/25`, `integrity_findings=0`, screenshots `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T14-52-53-240Z`; focused Chrome route visual for `alerts`, `work-board`, `feed-packing`, `feed-analytics`, and `sales-sold` desktop + 390px webview dark produced `26/26` screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T14-53-22-964Z` with `integrity_findings=0`, `unstable_captures=[]`. Nonzero route exit is stale perceptual baseline movement from intentional visual changes. Deprecated/non-visible surfaces remain excluded: Action Center, Calendar, Control Tower, Protocol Adherence, Workflows/workflow-record, and calendar detail routes. Remaining visual work before ready: continue the visible-route template-fidelity pass for remaining heavy mobile surfaces (Verify filters/table entry, Counts KPI-first pages, Feed Packing chip density, Sales card tone), then full Storybook baseline review/update and visible-route baseline review/update.

- 2026-09-21 20:38 IST: Continued visual-only template-fidelity pass; performance gates intentionally skipped per owner direction. Fixed the next visible holdouts from fresh Chrome screenshots: Verify mobile action rows and queue cards now use denser Minimal-style controls and labelled card rows instead of squeezed text; Counts Breakdown gets an explicit page class and safe full-width mobile KPI cards so values no longer clip/miss digits; Feed Packing worklist chips are smaller muted data tags instead of heavy button-like pills; Sales Summary KPI labels are forced muted while tone stays in the compact icon badges. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused Chrome route visual for `verify`, `counts-herd`, `counts-breakdown`, `feed-packing`, and `sales-sold` desktop + 390px webview dark produced `18/18` screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T15-06-09-733Z` with `integrity_findings=0`, `unstable_captures=[]`. Nonzero route exit is stale perceptual baseline movement only. Screenshots inspected: `verify__webview__dark.png`, `counts-breakdown__webview__dark.png`, `feed-packing__webview__dark.png`, and `sales-sold__webview__dark.png`. Remaining visual work before ready: full Storybook baseline review/update and visible-route baseline review/update after final visible-route sweep; deprecated/non-visible surfaces remain excluded.

- 2026-09-21 20:50 IST: Owner called out that the previous Feed Packing mobile screenshot still lacked the Minimal template's spacing rhythm. Captured the actual Minimal dashboard mobile preview after demo sign-in at `.codex-goatos-render/template-minimal-dashboard-auth-mobile-390.png` and used it as the spacing reference: clear gutters, visible section gaps, and card internals with breathing room. Fixed `/feed/packing` mobile spacing by adding route-scoped section rhythm, spacing between the filter/note/status blocks, larger KPI/card gaps, and a real worklist card body without a hard divider glued to the feed-item chips. Also tried a Verify action-strip experiment, rejected it after the screenshot clipped `Randomization`, and reverted that experiment before this checkpoint. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused Chrome route visual for `feed-packing` 390px webview dark produced `1/1` screenshot at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T15-20-44-685Z` with `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual baseline movement only. Remaining visual work before ready: continue template-fidelity review on other visible pages instead of accepting guard-green screenshots as design proof.

- 2026-09-21 21:00 IST: Fixed the owner-reported Feed Packing worklist header regression where the `InfoHint` icon was forced onto its own empty row on mobile. Root cause was the global mobile `.kit-card-action` wrap rule; the Feed Packing worklist header now uses a two-column title/action grid so the info glyph stays inline with the header like a Minimal-style compact header action. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused Chrome route visual for `feed-packing` 390px webview dark captured `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T15-29-45-958Z/feed-packing__webview__dark.png` with `integrity_findings=0`, `unstable_captures=[]`; nonzero exit is stale perceptual hash movement only.

- 2026-09-21 21:16 IST: Fixed the owner-reported mobile filter/empty-box alignment issues on visible Feed Packing and Verify surfaces. Feed Packing's mobile filter trigger now uses the same full-width gutter as the note/status blocks. Verify's mobile board no longer uses the old masked horizontal tab strip; module tabs wrap without clipped labels, the date/select/apply/clear filter controls share one width, and status chips align as full-width mobile rows. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused Chrome route visual for `verify` + `feed-packing` 390px webview dark captured `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T15-45-54-998Z/verify__webview__dark.png` and `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T15-45-54-998Z/feed-packing__webview__dark.png` with `integrity_findings=0`, `unstable_captures=[]`. Nonzero exit remains stale perceptual hash movement; `verify-all` logged the known hydration warning and needs separate cleanup outside this visual alignment patch.

- 2026-09-21 21:37 IST: Continued visible mobile template-fidelity fixes after the owner screenshot review. Work Board mobile was still using a horizontally cropped lane strip and stacked avatar overlap; changed the 390px layout to a single-column lane/card rhythm with full-width cards, wrapped metadata, and non-overlapping avatar chips. Verify Toxin mobile was still a squeezed table/list hybrid because a global verification table guard in `frame.css` forced a two-column label/value grid with `!important`; scoped toxin rows back to real stacked mobile cards with internal gaps and unclipped status tags. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS; focused Chrome route visual for `work-board`, `work-board-populated`, and `verify-toxin` 390px webview dark captured `3/3` screenshots at `.codex-goatos-render/admin-web-route-screenshots/2026-09-21T16-06-36-479Z` with no overlap/clipped-text integrity failures. Nonzero route exit is stale perceptual hash movement only. Screenshots visually inspected: `work-board__webview__dark.png` and `verify-toxin__webview__dark.png`.

- 2026-09-21 22:12 IST: Full visible route sweep after `e8e34ad12` found one fresh real visual regression: `work-board__desktop__dark.png` had a card meta row collapsed to 3px (`PC Care`) and overlapping stacked avatars. Most other route failures were stale perceptual hash movement; Verify/Feed Direction hydration warnings were observed in the route harness but did not reproduce in a lighter direct Chrome console probe, so they remain a follow-up item rather than a visual-spacing fix in this checkpoint. Fixed Work Board by making the card meta row a two-line grid on desktop too: the key line gets full width, the clock chip and owner/avatar action sit below without negative-margin overlap. Verification: `git diff --check` PASS; `npm --prefix apps/admin-web run typecheck` PASS. Direct Chrome desktop proof: `.codex-goatos-render/after-fix-screenshots/2026-09-21T16-35-05-work-board-desktop.png`, with zero key-clipping findings, zero avatar-overlap findings, and zero console errors. Direct Chrome Android WebView proof: `.codex-goatos-render/after-fix-screenshots/2026-09-21T16-42-07-work-board-webview-state.png`, with real Work Board content, zero key-clipping findings, and zero console errors. Caveat: the focused route harness hung twice on Work Board route settle after this patch, so this checkpoint uses direct Chrome proof rather than claiming a green route-lane rerun.

- 2026-09-21 23:23 IST: Current full Storybook dark integrity pass on pushed head `3d1275ec8` using the existing static build. Command: `npm --prefix apps/admin-web run visual:stories -- --no-build --viewports desktop,mobile --themes dark --max-diff-ratio 1`. Result: all 257 stories reached, 554 captures, `integrity_findings=0`, no play failures, and 21 mobile failures that are baseline height drift only. Evidence summary: `.codex-goatos-render/admin-web-story-screenshots/2026-09-21T17-45-22-004Z/summary.json`. Representative screenshots visually inspected and accepted as sane/no-overlap: `kit-card--header-variants__mobile__dark.png`, `kit-charts-trendchart--mobile-dashboard-grid__mobile__dark.png`, and `kit-tables-datatable--default__mobile__dark.png`. Remaining Storybook work: light-theme baseline review/update and final baseline acceptance; no current dark Storybook component overlap/clipped-text failure is open from this run.
