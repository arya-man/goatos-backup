# Web frontend performance judgement — PR #295 (Tasks page Jira-style redesign)

Judge: web-frontend-performance. Date: 2026-09-18.
Worktree: `/Users/raviteja/mesha/.worktrees/tasks-jira-ui`, branch `feat/tasks-jira-ui-20260918`.
Nothing was committed, stashed, or `git add`-ed by this judge.

**Verdict: ship-able on performance grounds, after the P0 below (which was fixed mid-run and is
verified fixed). The page is not slow. It is heavier: `/tasks` gains +35.3 KB gzip of client JS
(+20.3%, the largest growth of all 63 routes) and every other route in the app gains ~+19 KB gzip.
Under Lighthouse's throttled mobile profile that costs a reproducible 2 points and +338 ms LCP.
CLS stays at 0, the search debounce works, there are no hydration mismatches, and the board does
not block the main thread.**

---

## 0. What "before" and "after" mean here, and how each number was obtained

The brief asked for Phase 1 (before) then Phase 2 (after the other agents settled). Two things
forced a change of baseline, and every number below is labelled accordingly.

| | What it actually is |
|---|---|
| **BEFORE** | `origin/main` @ `1095aabcc2ffa682137b003e881a4a48ba42c01b`, built from a clean `git archive` extraction into the scratchpad (no repo mutation, no new git worktree), served by `next start` on **:13310**. |
| **AFTER (P1)** | The worktree as it stood at 05:41, mid-edit. Recorded but **superseded** — the route was crashing (see D1). |
| **AFTER (P2)** | The settled tree @ `6a89db5f4` ("feat(tasks): jira-style kanban board and issue detail panel"), rebuilt clean, served by `next start` on **:13309**. **This is the "after" in every table below unless it says P1.** |

Two method points that matter for anyone re-measuring:

- **The port :13308 "live web" server is `next dev`, not a production server.** Its numbers are
  dev-mode (per-request compilation, unminified, no chunk splitting): `/tasks` took 18.6 s and
  LCP measured 31,764 ms there. I discarded all of it and built + served production on :13309
  and :13310 instead. Do not quote dev-server timings as page performance.
- **The `origin/main` baseline server on :13307 was dead** (HTTP 500 on every route, including
  `/`). I did not revive it; I built my own baseline instead, as described above.
- **Next 16.2.9 no longer prints the size columns** in its route table — the `Route (app)` output
  is route names only, with no "First Load JS". So per-route bundle sizes below are computed
  directly from each route's `page_client-reference-manifest.js` (union of its client chunks,
  measured on disk, raw and gzip -9). This is more precise than the old table, but it is a
  *different* measurement than Next's historical "First Load JS" figure — compare like with like.

Tooling: Playwright 1.61 + `chromium_headless_shell` from the worktree root `node_modules`;
`PerformanceObserver` for LCP / layout-shift / longtask installed via `addInitScript` before any
page script; CDP `Network.loadingFinished.encodedDataLength` for true wire bytes (HTTP
`content-length` is absent on this server because responses are compressed/chunked — an early
pass that trusted `content-length` reported ~1.4 KB per page and was wrong).

---

## 1. Production build cost — CONFIRMED

| | Result |
|---|---|
| `origin/main` baseline build | **success**, exit 0 |
| Worktree build, attempt 1 (05:42) | `next build` **compiled successfully in 9.8 min**, then the `build-with-provenance.mjs` wrapper threw `HEAD changed during admin-web build` — another agent committed mid-build. The compile itself was fine. |
| Worktree build, attempt 2 (Phase 2, settled tree) | **success**, exit 0, compiled in 20.5 s (warm webpack cache) |
| Token-leak guard | skipped both times: `GOATOS_BEARER_TOKEN is not set` in the build env |

The retry the brief anticipated was needed, and the only failure was the concurrency guard, not the code.

### Per-route client JS (gzip, computed from client-reference manifests)

| Route | before | after | delta | delta % | raw delta | chunks |
|---|---|---|---|---|---|---|
| **`/tasks`** | 174.0 KB | **209.3 KB** | **+35.3 KB** | **+20.3%** | +127.1 KB | 16 → 18 |
| **`/tasks-preview`** | 173.7 KB | 208.6 KB | +34.9 KB | +20.1% | +125.7 KB | 16 → 18 |
| `/vaccination/plan` | 187.0 KB | 202.4 KB | +15.5 KB | +8.3% | +49.2 KB | 17 → 18 |
| `/action-center` | 171.6 KB | 186.9 KB | +15.3 KB | +8.9% | +49.2 KB | 15 → 16 |
| `/sales` | 172.8 KB | 188.1 KB | +15.3 KB | +8.9% | +49.2 KB | 15 → 16 |
| `/calendar` | 216.1 KB | 231.4 KB | +15.3 KB | +7.1% | +49.3 KB | 20 → 21 |
| `/routines` | 214.2 KB | 229.4 KB | +15.2 KB | +7.1% | +49.4 KB | 18 → 20 |
| **all 63 routes** | — | — | min +15.1 / **median +15.3** / max +35.3 KB | | | |

Decomposition, which is the useful way to read the above:

- **Globally shared JS** (the chunk set common to all 63 routes): 171.3 → **186.6 KB gzip**,
  **+15.3 KB (+8.9%)**; raw 562.8 → 612.0 KB. Every route in the app pays this. It is the new
  notification bell / web-push client landing in the shared layout (firebase-messaging-bearing
  chunks go from 1 to 3).
- **`/tasks`-specific growth**: +35.3 − 15.3 = **~+20.0 KB gzip** for the board, detail panel and
  filter toolbar.

Context so the outlier is judged fairly: `/tasks` is the **largest grower of all 63 routes**, at
2.3× the median. But in absolute terms 209.3 KB gzip is still **mid-pack** — `/calendar` (231.4)
and `/routines` (229.4) are heavier. `/tasks` did not become the heaviest route; it became
noticeably heavier than it was.

---

## 2. The shared global stylesheet — CONFIRMED, and a correction to the brief

The brief named `app/layout.css`. **There is no `app/layout.css` in this app.** The stylesheet
every route pays is **`app/mesha-theme.css`** (plus a small `app/globals.css`).

| | before | after | delta |
|---|---|---|---|
| Source `app/mesha-theme.css` | 455,991 B | 498,836 B | **+42,845 B (+9.4%)**, +470 / −6 lines |
| Built CSS per route, raw | 360.2 KB | 386.6 KB | +26.3 KB |
| Built CSS per route, **gzip** | 63.0 KB | **67.1 KB** | **+4.1 KB (+6.5%)** |
| Over the wire (CDP, both viewports) | 66,126 B / 2 files | 70,362 B / 2 files | +4,236 B |

The PR appends ~470 lines of new `.lt-*` / `.ltb-*` / `.ltd-*` rules (board, card, detail panel,
filter toolbar). **This is not the defect the brief worried about:** +4.1 KB gzip on every route
is a modest, proportionate cost for a feature of this size, and it compresses well (9.4% of
source becomes 6.5% of gzip). Reported, not flagged.

---

## 3. Route weight over the wire — CONFIRMED

Playwright + CDP `encodedDataLength`, production servers, single clean load each, both viewports.

| Route / viewport | requests | total wire bytes | delta |
|---|---|---|---|
| `/tasks?scope=team_progress` @1440×900 | 21 → **25** | 564,763 → **596,470** | **+31,707 (+5.6%)** |
| `/tasks?scope=team_progress` @390×844 | 21 → **25** | 564,763 → **596,028** | +31,265 (+5.5%) |
| `/tasks-preview` @1440×900 | 19 → **23** | 348,072 → **404,447** | **+56,375 (+16.2%)** |
| `/tasks-preview` @390×844 | 19 → **23** | 348,072 → **404,447** | +56,375 (+16.2%) |

By type, `/tasks` @1440×900:

| type | before | after |
|---|---|---|
| Document (HTML + RSC payload) | 226,023 B / 1 | 204,268 B / 1 (**−21,755**, smaller) |
| Script | 271,545 B / 16 | 320,232 B / 19 (+48,687) |
| Stylesheet | 66,126 B / 2 | 70,362 B / 2 (+4,236) |
| Fetch | 1,069 B / 1 | 1,608 B / 2 |

Largest individual assets after (`/tasks` @1440×900): the HTML document **204,268 B**, then
`chunks/9221-*.js` 69,870 B, the shared stylesheet **64,608 B**, `chunks/87c73c54-*.js` 63,341 B,
`chunks/1968-*.js` 62,351 B.

Worth noting in the PR's favour: **the document itself got 21.8 KB smaller** (the board renders 25
cards where the old table rendered 50 rows). The weight increase is entirely JS + CSS, not payload.

---

## 4. Render timing in a real browser — CONFIRMED. No regression; several metrics improved.

Playwright, production builds, **median of 6 loads** per route per viewport, unthrottled over
localhost. All times in ms. `firstRow` = wall-clock from `goto()` to the first task row/card
being *visible*.

### `/tasks?scope=team_progress` @ 1440×900

| metric | before | after | delta |
|---|---|---|---|
| DOMContentLoaded | 286.8 | **228.0** | −58.8 |
| load | 287.3 | **230.7** | −56.6 |
| First Contentful Paint | 486 | **464** | −22 |
| Largest Contentful Paint | 754 | **736** | −18 |
| **Cumulative Layout Shift** | **0** | **0** | **0** |
| Total Blocking Time | 0 | 0 | 0 |
| first task row visible | 509.5 | **476.0** | −33.5 |
| LCP p95 | 828 | 784 | −44 |
| long tasks > 50 ms | none | one, 50.0 ms | +1 (borderline) |
| rows rendered | 50 (table) | 25 (board cards) | — |

### `/tasks?scope=team_progress` @ 390×844

| metric | before | after | delta |
|---|---|---|---|
| DOMContentLoaded | 361.4 | **193.7** | −167.7 |
| load | 361.8 | **195.0** | −166.8 |
| First Contentful Paint | 516 | **438** | −78 |
| Largest Contentful Paint | 776 | **696** | −80 |
| **Cumulative Layout Shift** | **0** | **0** | **0** |
| Total Blocking Time | 0 | 0 | 0 |
| first task row visible | 543.5 | **471.5** | −72.0 |
| LCP p95 | 1388 | **700** | −688 |
| long tasks > 50 ms | none | none | — |

### `/tasks-preview`

| metric | 1440 before → after | 390 before → after |
|---|---|---|
| FCP | 324 → 318 | 284 → 320 |
| LCP | 328 → **666** | 358 → 320 |
| CLS | 0 → **0** | 0 → **0** |
| TBT | 0 → 0 | 0 → 0 |
| first row visible | 412 → 420 | 436 → 413.5 |

**On CLS specifically — the axis the brief was most worried about: measured 0.000 before and
0.000 after, at both viewports, on both routes, across 6 loads each, with the observer armed
before first paint and a 2.5 s post-load settle window to catch late shifts. The chip counts and
the filter toolbar do not reflow after the payload arrives. `biggestShifts` was empty in every
single sample. This is clean and is not a concern.**

**On the board and the main thread: also clean.** The board renders **25 cards** (the list
endpoint caps `limit` at 25 on this path), not the 50-per-column the brief feared. TBT is 0
unthrottled; the single 50.0 ms long task at 1440×900 is exactly at the threshold and appeared in
one sample. This does not block the main thread.

**Mobile is not worse than desktop — it is better.** Every 390×844 metric improved, and LCP p95
at 390 improved by 688 ms. The "fine at 1440, bad at 390" failure mode does not occur.

---

## 5. Lighthouse, throttled mobile — CONFIRMED. This is the one place the PR measurably slows down.

`npm run perf:lighthouse` (the repo's own harness), Lighthouse 13.4, default mobile throttling,
**median of 3 runs per arm**. Scores were identical across runs (before 83/83/83, after 81/81/81),
so this is reproducible, not noise.

| metric | before | after | delta | delta % |
|---|---|---|---|---|
| **performance score** | **83** | **81** | **−2** | −2.4% |
| First Contentful Paint | 2266 | 2266 | −1 | −0.0% |
| **Largest Contentful Paint** | 4164 | **4502** | **+338** | **+8.1%** |
| **Cumulative Layout Shift** | **0** | **0** | **0** | — |
| **Total Blocking Time** | 63 | **86** | **+23** | **+36.5%** |
| Speed Index | 2266 | 2453 | +186 | +8.2% |
| Time to Interactive | 4171 | 4502 | +331 | +7.9% |
| Max Potential FID | 84 | 105 | +21 | +25.0% |
| Main-thread work | 977 | 1046 | +70 | +7.2% |
| Script bootup time | 411 | 421 | +10 | +2.5% |
| Total byte weight | 582,877 | 614,142 | +31,265 | +5.4% |

Read this together with §4, because the two are not in conflict — they answer different
questions. Unthrottled on localhost the extra JS costs nothing and the page is actually faster.
Under a throttled mobile CPU and network, the extra ~35 KB gzip of JS has to be fetched, parsed
and executed, and that shows up as +338 ms LCP and +23 ms TBT. **The throttled profile is the one
that resembles a park manager on a phone**, so it is the number to take seriously. TBT at 86 ms is
still comfortably inside Lighthouse's "good" band (<200 ms), and CLS holds at 0.

---

## 6. Interaction responsiveness — CONFIRMED (after only; no "before" exists)

**There is no before number for search: `origin/main`'s tasks page renders no
`input[type=search]` at all.** The search box, the chips and the filter toolbar are new in this
PR, so these are absolute measurements, not deltas.

### Search debounce — works, and bounds the endpoint correctly

| measurement | result |
|---|---|
| characters typed | 10 (`brucellosi`) |
| **RSC requests fired** | **exactly 1** |
| URL fired | `/tasks?scope=team_progress&t_q=brucellosi&_rsc=…` (the full 10-char query, once) |
| first keypress → input reflects it | **24 ms** |
| last keypress → request dispatched | 305 ms |
| first keypress → request dispatched | 395 ms |

**The debounce does protect the endpoint: 10 keystrokes produce one request, carrying the final
query, ~305 ms after typing stops.** With the API at ~230–285 ms this is the correct shape. No
defect.

### Other interactions (@390×844 unless noted)

| interaction | control found | click → settled |
|---|---|---|
| open filter sheet | `.lt-fmore` | 473 ms |
| open notification panel | `[aria-label*=notification]` | 426 ms |
| select a task card | `.ltb-card` | 424 ms |

**Method caveat, stated plainly: these figures include a fixed 400 ms settle wait built into my
harness, so the actual handler latency is roughly `value − 400` ≈ 24–73 ms.** I am not going to
dress that up as a p50/p95 — it is one sample per control and the instrument is coarser than the
thing being measured. What it does establish is that every control is present, reachable and
responds without a stall. p50/p95 are reported only for the load metrics in §4, where I have 6
samples.

---

## 7. Hydration and client JS — CONFIRMED clean

| | before | after |
|---|---|---|
| `"use client"` in `features/leadership-tasks` | 1 (of 5 modules) | **9 (of 20 modules)** |
| `"use client"` in `features/notifications` | 0 (feature does not exist on main) | **3 (of 10 modules)** |

The four modules the redesign adds are all **server** components — `leadership-tasks-board.tsx`,
`task-board-card.tsx`, `task-detail-panel.tsx`, `task-presentation.ts`. That is the right call
architecturally and is why the board costs no hydration.

Console output, per route, per viewport, across 6 loads each:

| route / viewport | errors | warnings | hydration mismatches | react-hooks errors |
|---|---|---|---|---|
| `/tasks` @1440×900 | 1 | 0 | **none** | **none** |
| `/tasks` @390×844 | 1 | 0 | **none** | **none** |
| `/tasks-preview` @1440×900 | 0 | 0 | **none** | **none** |
| `/tasks-preview` @390×844 | 0 | 0 | **none** | **none** |

The single error on `/tasks` is `GET /api/auth/firebase-config → 503`. **It occurs identically in
the `origin/main` arm**, so it is this local stack having no Firebase config, not a PR regression.
No warning storm. No hydration mismatch. Nothing to flag.

---

## 8. The repo's own harnesses

| harness | ran? | detail |
|---|---|---|
| `npm run perf:lighthouse` | **YES** | Ran 3× per arm. Needs `ADMIN_WEB_LIGHTHOUSE_URL` plus `ADMIN_WEB_LIGHTHOUSE_EXTRA_HEADERS='{"Cookie":"goatos_firebase_id_token=<token>"}'` (it authenticates by cookie, not bearer header). Set `ADMIN_WEB_LIGHTHOUSE_MIN_SCORES=performance=0` to capture rather than gate. Results in §5. |
| `npm run perf:pagespeed` | **NO — cannot** | `capture-pagespeed.mjs` calls Google's PageSpeed Insights API, which must fetch the URL **from the public internet**. `127.0.0.1:13309` is unreachable to Google. It also requires `PAGESPEED_API_KEY`, which is not set. This harness is structurally inapplicable to a local stack; it is not a skip. |
| `npm run smoke:visual:live` | **NO — cannot, two independent blockers** | (1) `smoke-visual-live.mjs:293` requires `GOATOS_LOCAL_STACK_RECEIPT_FILE` for any localhost run that is not narrowed to `work-board`/`weighing` — that receipt is emitted only by `run-local-next`, and **no receipt file exists** in the session scratchpad. (2) Even with one, `validateLocalStackReceipt` pins the receipt's `git_sha` to the API's `/version` `build_sha`, and **the running API reports `e10a26c9508d6ba89d4034ca4cdc409a16035fc7`, which is 9 commits behind `HEAD` (`6a89db5f4`)** — so the provenance gate would fail on sha mismatch. The API *does* report a real sha (not `unknown`/`dev`), so gate (1) is the binding one. |

**Carry this caveat into the PR doc:** the API binary serving :18088 is 9 commits behind HEAD and
therefore **predates commit `50a7c89b9` "perf(tasks): collapse the list endpoint from ~10 round
trips to ~5"**. Every server-side latency figure here (API 236 ms direct, 462 ms as seen by the
web server) is from the *un-optimised* endpoint. The frontend numbers in §1–§7 are unaffected —
they measure bundles, paint and interaction, not API latency — but do not quote the API timings as
evidence about that commit.

---

## 9. Ranked defect list

### D1 — P0, FOUND AND FIXED DURING THIS RUN, verified fixed. `initials()` / `statusTone()` called across the client→server boundary, killing the whole route.

The three new **server** components imported two helpers from `leadership-tasks-table.tsx`, which
is a `"use client"` module. A function exported from a client module is a *client reference*, not
a callable function on the server. 8 call sites:

```
leadership-tasks-board.tsx:7   import { initials } from "./leadership-tasks-table";   (:243)
task-board-card.tsx:7          import { initials } from "./leadership-tasks-table";   (:58)
task-detail-panel.tsx:14       import { initials, statusTone } from "./leadership-tasks-table";
                                                              (:143, :214, :315)
```

**The measurement that proves it:**

- Production server log (`next start`, not dev): **206 occurrences** of
  `⨯ Error: Attempted to call initials() from the server but initials is on the client.`
- Browser, all four route/viewport combinations: **0 task cards, 0 rows**, and the user-visible
  text was `"Something went wrong — This screen failed to render. The error has been reported."`
  (`Error reference: 3309806185`); React error **#419** (server render aborted) on `/tasks-preview`.
- Not a dev-mode artefact: reproduced identically in the optimised production build, and in
  `next dev` on :13308.
- **Not an API problem** — this is worth stating because the page's own message said "Tasks could
  not be loaded". The API was healthy throughout: `GET /app/leadership-tasks?scope=team_progress`
  returned **200 in 462 ms with all 424 tasks** (`all 424 / open 189 / in_progress 118 / done 117`)
  in the same request that then failed to render.
- `origin/main` on the same stack rendered **51 `<tr>`** in 0.43 s with zero errors — so this was
  introduced by the PR, not inherited.

**Fixed at 06:21 while this judgement was running**: the helpers were moved into a new server-safe
`features/leadership-tasks/task-presentation.ts` (no `"use client"`), and all four call sites
re-pointed at it. **Verified in the Phase 2 clean rebuild: 0 server errors, 70/70 responses 2xx,
25 board cards, toolbar and search box present, no "failed to render".**

Recorded because it reached a build and would have been a total outage of `/tasks` and
`/tasks-preview`, and because the guard that would catch it belongs in CI: a server component
importing from a `"use client"` module is statically detectable and nothing in the repo's
`check:*` suite currently looks for it.

### D2 — P1. `/tasks` First Load JS grows +35.3 KB gzip (+20.3%), the largest of all 63 routes.

Proof: §1. 174.0 → 209.3 KB gzip, raw +127.1 KB, 16 → 18 chunks, computed from
`page_client-reference-manifest.js` against a clean `origin/main` build. Median growth across the
other 62 routes is +15.3 KB, so ~+20.0 KB of this is `/tasks`-specific. Mitigating: 209.3 KB gzip
is still below `/calendar` (231.4) and `/routines` (229.4). This is the direct cause of D4.

### D3 — P1. Every one of 63 routes pays ~+19.4 KB gzip for the new global notification/push client.

Proof: §1 and §2. Globally shared JS 171.3 → 186.6 KB gzip (+15.3, +8.9%); shared CSS 63.0 → 67.1
KB gzip (+4.1). firebase-messaging-bearing chunks go from 1 to 3. Flagged not because it is
disproportionate — a notification bell in the global chrome legitimately costs something — but
because it is an app-wide cost incurred by a Tasks PR, and the reviewer should agree to it
knowingly. If the push client can be lazily imported behind the bell's first interaction rather
than eagerly in the shared layout, most of the +15.3 KB comes back on 62 routes that never use it.

### D4 — P2. Reproducible Lighthouse regression on throttled mobile: LCP +338 ms, TBT +23 ms, score −2.

Proof: §5, median of 3 runs per arm, identical scores across all runs (83/83/83 vs 81/81/81).
Consequence of D2/D3, not an independent fault. Below the level at which I would block a release —
TBT 86 ms is inside the "good" band and CLS is 0 — but it is the honest answer to "does this make
the dashboard slower", and it is the number to re-measure after any bundle work.

### Cleared — measured and fine, one line each

- **CLS**: 0.000 before, 0.000 after, both viewports, both routes, 6 loads each, no shifts recorded at all. No toolbar or board reflow after data arrives.
- **Search debounce**: 10 keystrokes → exactly 1 request. Endpoint properly bounded.
- **Console**: no hydration mismatches, no `react-hooks` errors, no warnings; the one error (`/api/auth/firebase-config` 503) is present on `origin/main` too and is local-stack config.
- **Long tasks / board rendering**: board renders 25 cards, not 50; TBT 0 unthrottled, one 50.0 ms task at 1440×900. Main thread is not blocked.
- **390×844 vs 1440×900**: mobile improved on every load metric (LCP p95 −688 ms). No mobile-specific regression.
- **Global stylesheet**: +4.1 KB gzip (+6.5%) per route for ~470 new lines. Proportionate; reported in §2, not flagged.
- **Document/payload size**: the `/tasks` HTML document shrank 21.8 KB (25 cards vs 50 rows).

---

## 10. Re-measuring this later

```bash
# 1. Baseline, without touching the repo or creating a git worktree:
S=/tmp/perfbase; rm -rf $S && mkdir -p $S
git archive origin/main | tar -x -C $S
ln -s /Users/raviteja/mesha/.worktrees/tasks-jira-ui/node_modules $S/node_modules
cd $S/apps/admin-web && next build --webpack          # ~10 min cold

# 2. Serve both arms in production mode (NOT next dev — see §0):
#    env: GOATOS_ENV=local GOATOS_AUTH_MODE=bearer GOATOS_AUTH_ISSUER=goatos-local
#         GOATOS_AUTH_AUDIENCE=goatos-api GOATOS_API_BASE_URL=http://127.0.0.1:18088
#         GOATOS_TENANT_ID=<tenant> GOATOS_BEARER_TOKEN=<token>
next start -H 127.0.0.1 -p 13310   # baseline
next start -H 127.0.0.1 -p 13309   # candidate

# 3. Bundle sizes (Next 16 prints none):
node extract-bundle.cjs <buildRoot>/apps/admin-web out.json

# 4. Browser metrics and interactions:
node perf-measure.mjs          http://127.0.0.1:1330X <label> 6
node perf-weight-interact.mjs  http://127.0.0.1:1330X <label>

# 5. Lighthouse (cookie auth, not bearer):
ADMIN_WEB_LIGHTHOUSE_URL="http://127.0.0.1:1330X/tasks?scope=team_progress" \
ADMIN_WEB_LIGHTHOUSE_EXTRA_HEADERS='{"Cookie":"goatos_firebase_id_token=<token>"}' \
ADMIN_WEB_LIGHTHOUSE_CATEGORIES=performance \
ADMIN_WEB_LIGHTHOUSE_MIN_SCORES=performance=0 npm run perf:lighthouse
```

Scripts and raw JSON used for this report, all in the session scratchpad:
`perf-measure.mjs`, `perf-weight-interact.mjs`, `extract-bundle.cjs`, `find503.mjs`,
`bundle-before.json`, `bundle-after-p2.json`, `perf-prod-before.json`,
`perf-prod-after-p2.json`, `weight-before.json`, `weight-after-p2.json`,
`lh-before{,-2,-3}.json`, `lh-after{,-2,-3}.json`, and the build logs
`build-before-main.log`, `build-after-1.log`, `build-after-p2.log`.

**Gates worth adding to CI, in priority order:** (1) a static check that no server component
imports a value from a `"use client"` module (would have caught D1 before it ever built);
(2) a per-route gzip bundle budget, seeded at the numbers in §1, so D2-class growth is visible in
review rather than discovered by a judge.
