# Dashboard Nightly Check-and-Fix Automation — Plan v2

Target: https://dashboard.mesha.sg (apps/admin-web, Next.js) · API https://api.goatos.mesha.sg · goatos-stg DB
Runner: Ravi's always-on personal PC, cron/launchd at 02:00 Asia/Singapore
Repo: vgoats/goatos · tooling dir `tools/sentinel/` (rename freely)
Date: 2026-09-19 · v2.4 (everything nightly; full DB read from Ravi's PC, no data restrictions) after three independent reviews + two rounds of Ravi's review (credentials, healer sandbox, cold vs steady latency ordering, HAR expectations, leak scan, notify-secret provisioning; DB = full read from the PC per Ravi)

---

## 0. Why (what the history says)

Four agents read the diffs of all 3,371 commits since 2026-08-01. The same things regress because every
fix patches a feature and never the shared thing underneath:

| Symptom | Re-fixed | Shared root never fixed |
|---|---|---|
| Error boundary "Something went wrong / This screen failed to render" | 13 commits; `lib/admin-ui-contract.ts` patched 8x | `admin-ui-contract.ts` has 5 `throw` sites on any missing copy key / option / icon token inside server components -> whole route down |
| Chart empty / wrong series count / labels clipped | herd-signals drawer chart 5x in one day; month charts 3x in one day; "Over 35 kg" footer 3x in 3 days | Charts are NOT Recharts — 6 hand-rolled SVG components (`svg-series`, `svg-bars`, `svg-column-bars`, `grouped-columns`, `month-columns`, `hbar-list`), each with own empty-detection/padding/label scale; global CSS collisions (`.bar`) |
| Phone-width overflow, hidden controls, tables not scrolling | 16 commits; same `overflow-x` rule added to 7 wrapper classes separately | `app/mesha-theme.css` — 243 touches since Aug 1, ~4,000 lines, one global file; scroll owner per-feature not in `DataTable` |
| API p95 > 500 ms | Work Board 10x (Sep 10–16), vaccination command 6x, weights landing 7x | cache keys with nanosecond timestamps (never hit, twice), serial awaits, unbounded fan-out, caches hiding 1.6 s uncached reads |
| Wrong numbers vs DB | feed forecast, farm value (16 ICU-Kid dropped), farm-born revenue (all deal statuses), All-parks order 3x | aggregate-after-decorate; `DISTINCT ON` without total order; stale in-process caches across Cloud Run instances |
| **Second-step bugs (found by the coverage review — not in v1)** | permission/scope/role ~147 subject hits; proof media ~162; filters ~70; save/publish ~69 | only appear after a click, a save, a different role, or a real WebKit engine |

Existing guards miss these because ~30 Makefile guards regex the *source*, and the one live sweep
`apps/admin-web/scripts/smoke-visual-live.mjs` (1,786 lines, ~115 routes, 1440 + 390) only started failing
on error-boundary text on 2026-09-17, has no chart/clipping/NaN/console/hydration assertions, no
interaction assertions, one persona, and Chromium with a spoofed iPhone UA (not WebKit).
**Correction from v1:** it CAN run against live today with `GOATOS_ADMIN_WEB_BASE_URL`,
`GOATOS_API_BASE_URL`, `GOATOS_BEARER_TOKEN`, `GOATOS_TENANT_ID`; what is missing is token minting,
bootstrap-derived routes, HAR capture, and the assertions. `review_bugs_ledgers.md` last updated 2026-07-24.
No "judge" exists in the repo.

---

## 1. Architecture

```
02:00 SGT  cron/launchd on Ravi's PC  ->  tools/sentinel/nightly.sh   (timeout 3h, heartbeat every stage)
  |
  |-- Stage 0  PREFLIGHT  local only: claude -p "ok", gh auth status, secrets unlocked (no API traffic yet)
  |                       API /readyz, /version SHA and token mint run right after Stage 1a
  |-- Stage 1a COLD       latency first-hit on last night's endpoint manifest, BEFORE any browser/API
  |                       traffic, against scaled-to-zero Cloud Run; every first response < 500 ms
  |-- Stage 1  PROBE      deterministic node scripts, no LLM  (make sentinel-probe)
  |     UI sweep         every route/state x {laptop 1440x1000, phone 390x844} x 4 personas
  |     Interactions     filters, pagination, save-in-place, drawers, media play, date pickers, deep links
  |     Data             Layer 1 chart<->API (nightly) · Layer 2 API<->DB and Layer 3 facts (see §2.2)
  |     Latency 1b       steady-state on tonight's HAR endpoints, p95<=500, AFTER the sweep (warm)
  |     Auth/Firebase    session, proxy gate, media proxy, token leak
  |     Real-user errors Faro/Loki last-24h error diff
  |     Ledger           one tripwire per fix since Aug 1
  |     Coverage         what is new and not yet covered
  |     -> report.json, screenshots (evidence only), HAR, run.log
  |-- Stage 2  TRIAGE     flake retry · env-down stop · cluster · quarantine · heal-eligibility
  |-- Stage 3  HEAL       claude -p, only for heal-eligible clusters, hard caps (§4)   [OFF for first 14 nights]
  |-- Stage 4  PR         one PR autofix/YYYY-MM-DD -> main; never merges
  '-- Stage 5  REPORT     always sent, even on total failure; dead-man alarm if no "end" by 05:00
```

Hard rules, enforced mechanically (hooks + scripts, not prompt hope): read-only DB; no `gcloud`; no deploy;
no `gh pr merge`; no push to `main`/`stg` (existing `check-stg-promotion` / `check-local-ci-evidence`
hooks); healer cannot edit probes, thresholds, ledger, tests, Makefile, `.claude/`, lockfiles (§4);
budget/turn/wall-clock caps; quarantine after 3 nights.

---

## 2. Stage 1 — Probes

### 2.1 UI sweep (extend `smoke-visual-live.mjs`; do not rewrite)
Add:
- Routes from `/admin-web/bootstrap` nav ∪ `app/(admin)/**/page.tsx` (mismatch = finding); dynamic ids resolved from stg API (existing fixture lookups).
- Token minting: Identity Toolkit `signInWithPassword` (apiKey from `/api/auth/firebase-config`) -> set `goatos_firebase_id_token` + `goatos_firebase_refresh_token`; re-mint every 45 min.
- **4 personas**: admin/CEO, verifier, park_head (one park), director. Every sidebar leaf must open without 403/boundary; no `disabled` primary button on a page the persona is ticked for; Work Board / Verify rows within the persona's park set; lacking permission -> 403 JSON not 500. Reuse `tools/dev/audit-person-access.py`.
- **Real WebKit** Playwright project (`devices['iPhone 14']`, engine webkit) for top-10 routes + Verify drawer + one video play. Chromium mobile keeps the full sweep.
- HAR recording (`recordHar`) per route/state.
- Determinism: pinned Playwright Chromium (`GOATOS_SMOKE_BROWSER_CHANNEL=chromium`), `prefers-reduced-motion`, animations disabled, TZ `Asia/Singapore`, locale `en-SG`, `document.fonts.ready`.
- Assertions (from the Aug–Sep diffs):
  - **error boundary**: visible text must not match `/Something went wrong|This screen failed to render|missing copy key|Application error|could not be loaded|backend_down/`; `pageerror` empty; console free of `Hydration|did not match|two children with the same key`.
  - **chart rendered**: per card with `svg[role=img]`: legend items <= drawn series; `rect[height>0]|path` > 0 unless explicit empty-state label; no x-axis past today.
  - **clipping**: every `svg text` bbox inside viewBox; `.wbv`, `.gcval`, `.kpi .val` `scrollWidth <= clientWidth` and inside their card; drawer `scrollWidth <= clientWidth`; inject a 200-char title into one drawer and re-measure.
  - **numbers**: visible `innerText` free of `NaN|undefined|Infinity|null`, negative day counters; no ISO `\d{4}-\d{2}-\d{2}(T…)?` in visible text outside `input[type=date]`, `time[datetime]`.
  - **phone**: `documentElement.scrollWidth <= 390`; interactive targets >= 40x40 and `right <= 390` (low-yield, never heal budget); `.side.open` covers main; `table` with `scrollWidth > 392` has an `overflow-x` owner whose `scrollLeft` moves.
  - **loading**: no global `app/loading.tsx` "Loading Mesha admin data" after sidebar click; skeleton inside `main .screen`.
  - **sub-requests**: HAR assertions are route- and persona-aware. Expected statuses are declared per
    (route, persona): `/` unauthenticated -> 302 `/login`; permission-denied -> 403 JSON with error shape;
    `/api/proof-media/*` -> 307 to a signed same-origin URL. Those are checked for shape and destination;
    any **undeclared** 4xx/5xx fails.
  - **theme**: axe colour-contrast >= 4.5 in dark (default; theme is not persisted so real usage is dark); light theme nightly as well (secondary priority in the report).
- Screenshots: full page per route/state/viewport as **evidence only** (before/after in PR). **No pixel-diff gate** (real data changes every bar; 243 CSS touches/7 weeks -> permanent red). Optional later: data-masked layout snapshot (bboxes of cards/nav/tables, 2 px tolerance).
- Playwright config: retries=1, trace+video on failure.

### 2.1b Interaction probes (new — the biggest uncovered class)
Run in a **sandbox tenant on stg** (does not exist today; must be created — §9). Until it exists, write
probes are skipped, not faked.
1. **Filter applies**: click each chip/select -> `location.search` changed, row count or "N results" changed, `aria-pressed/.on` moved, HAR request carries the param; every facet option returns >= 1 row.
2. **Pagination**: page 2 rows != page 1; lane/header count unchanged; no empty-state while count > 0; two pagers on one page use distinct params (`fa_offset` collision).
3. **Save lands in place**: record `main.scrollTop`, `history.length`, `location.search` before submit; after success text: all unchanged, edited row still `aria-current` and in view; no bounce to `/login`.
4. **Selection**: click row -> detail panel contains that row's identifier; after save still selected.
5. **Proof media**: open every drawer/lightbox with media; `video.readyState>=2 || img.naturalWidth>0` within 5 s; every media `src` is same-origin `/api/proof-media/…` with 2xx/307, never bearer-only API 401.
6. **Date pickers**: popover bbox inside viewport and not clipped by `overflow:hidden` ancestor; `color-scheme` matches theme; named window updates `?from/?to`.
7. **Deep links**: `/?park=X&as_of=D` survives redirect; stale ids -> soft empty/redirect, not 404/boundary.
8. **Control targets exist**: every `form[action]` / submit hits a live route (no 404/405 in HAR); run `TestEveryControlActionIsARealRoute` against the live contract.
9. **Dialogs**: Esc closes, focus returns, 30 Tabs stay inside.
10. **Replay**: same form submitted twice -> identical 2xx (idempotency 500s).
11. **Empty-state truth**: if an empty label is shown, the Layer 2 oracle count must be 0.
12. **Ops pages**: each audit/DLQ family tab count > 0 when DB has rows for that domain in range.
13. **CEO-AI**: 10 golden questions with numeric answers from Layer 2 oracles; "all parks" must not carry `park_id`.

### 2.2 Data validation — three layers
**Layer 1 Chart <-> API (frontend; nightly; heal-eligible).** Intercept JSON per page; read rendered DOM back:
legend == drawn series; bars == data points (minus explicit empties); every visible value label == JSON
value formatted; stacked segments == total; KPI tiles == JSON fields.

**Layer 2 API <-> DB (backend; nightly; never heal-eligible -> issue with SQL + JSON + diff).** Independent oracle
SQL (raw `count/sum`) per hot endpoint. Seed 8 (from the fixes): Work Board lane sums; alive animals
(counts == farm-value valued+excluded); loadwise one-load-per-goat + per-load purchased; farm-born revenue
`Deal Closed` only; feed forecast kg>0 on last 3 locked issues, unverified sheds null not 0; herd-signals
stale count; ADG pairs inside window; `/version` `migration_drift=false`.
- Consistency (review finding): only endpoints with an explicit `as_of`/`to` param get exact comparison
  with the same literal. Otherwise: API JSON captured, oracle run within the same second, mismatch re-run
  once; only a reproduced mismatch is reported. Time-relative facts ("stale", "due today") get a tolerance
  window equal to the run's duration.
- Tolerances: counts/dates exact; money/kg ±0.5 %; cached reads must agree within TTL (30 s).
- **DB access — full, nightly, from Ravi's PC. Owner's decision, no restrictions.** Ravi is the CEO,
  it is his machine and his company's data; the automation reads **anything in goatos-stg** it needs.
  A service account `goatos-sentinel@goatos-stg` (`roles/cloudsql.client`) with its key on the PC, used by
  `cloud-sql-proxy` on `127.0.0.1:5456`, logging in as role `sentinel_ro` = `pg_read_all_data` (every
  table, every column, every schema). The only property kept is *read-only*
  (`default_transaction_read_only=on`, no DDL) — purely so a probe can never corrupt stg, not a data
  boundary. Oracles are plain SQL files in `tools/sentinel/oracles/*.sql`; adding one is a normal PR.
  Claude, in the nightly and in supervised sessions, may query any table directly to build oracles,
  facts and diagnoses. Documented in CLAUDE.md as the sentinel read identity (not a deployment authority).

**Layer 3 Cross-page facts (nightly; never heal-eligible).** `facts/*.yaml`: one entry per business fact, every
surface that shows it, oracle, tolerance, owning ledger entry. Seed: alive animals; animals per purchased
load; ADG/weight per window; vaccination due/done today; feed given vs directed per shed; sales revenue per
period; stale tags. Invariants: All parks == Σ parks; stacked == total; period == Σ months; percentages ==
100 ± rounding; "so far" <= "to date"; nothing past today. Report names the fact and the odd-one-out surface.
Facts are hand-curated with Ravi; the nightly only flags **candidate** pairs (same field name + unit +
scope keys across two endpoints) — Claude does not draft backend semantics.

### 2.3 Latency (reuse `tools/perf/api-latency-gate.mjs`; no k6)
- Manifest generated nightly from the sweep's HAR in the `hot-paths.*.json` schema (`{name, method, path, max_response_bytes, assertion}` + `manifest_sha256`/iterations/warmup/concurrency so `api-latency-compare` accepts it). Unfetched endpoints flagged dead.
- **Two signals, reported separately** — the gate already does this (`api-latency-gate.mjs` keeps warmup
  requests apart from steady-state and requires every warmup response < 500 ms, PR264):
  1. **Cold / first-hit — Stage 1a** (what a user sees at 06:00): the very first network calls of the
     night, before preflight touches the API (preflight's `/readyz` + `/version` are moved after it or
     excluded from the manifest), using **last night's** HAR-derived manifest; every first-hit response
     must be < 500 ms or it is a cold-path finding on its own (`docs/decisions/scale-anti-patterns.md`:
     caches must not hide uncached cost). Ordering is enforced in `nightly.sh`, not by convention.
  2. **Steady state — Stage 1b**: run **after** the sweep, `--warmup 5` (default), 20 iterations, **concurrency 1
     only**; policy p90<=300, p95<=500, p99<=500, <=1 MiB (`api-latency-policy.mjs`).
- Compare against the **median of the last 7 nights**, not last night; heal-eligible only when p95 > budget on **2 consecutive nights and drift > 25 %** (the 25 ms rule is nightly noise at 02:00 on scaled-to-zero Cloud Run).
- `measure-sidebar-click-latency.mjs` (`ADMIN_WEB_URL`, `GOATOS_BEARER_TOKEN`) for route transitions.
- Static guard (new, registered in `tools/ci/guardrail-manifest.json`): no `time.Now()`/RFC3339Nano in cache keys.

### 2.4 Lighthouse (nightly; warn-only trend)
`capture-lighthouse.mjs --extra-headers '{"Cookie":"goatos_firebase_id_token=…"}' --expect-final-url-contains`,
routes `/`, `/work-board`, `/vaccination`, `/sales`, `/feed/analytics`, mobile + desktop, **median of 3**.
Runs every night. Zero commits since Aug 1 fixed a Lighthouse-class issue, so perf budgets are trend/warn
(never heal-eligible); a11y contrast is a hard finding (maps to a real theme bug).

### 2.5 Auth / Firebase smoke (read-only)
1. `/livez`, `/readyz` 204; `/version` `migration_drift=false`, SHA recorded at start and end (mismatch -> discard run, retry after 10 min).
2. `/login` 200; `/` no cookie -> 302 `/login`; refresh-cookie-only -> 200 (`65e3c6132`).
3. `POST /api/auth/session` garbage idToken -> 401, no `Set-Cookie`.
4. `GET /api/auth/firebase-config` 200 with env's apiKey/projectId.
5. Password sign-in -> idToken; `/api/proof-media/<known stg id>` -> 307 not 401.
6. Secret/session leak: `check-token-leak.mjs` only greps the exact `GOATOS_BEARER_TOKEN` in a local
   `.next/` — run it nightly on a local `next build` of `origin/main` (incremental, ~5 min), and the nightly scan of fetched
   `_next/static` chunks, HTML, and RSC payloads is **pattern-based with an allowlist**: JWT shape
   (`eyJ[A-Za-z0-9_-]+\.eyJ…`), Firebase refresh tokens, `X-Tenant`/tenant ids in client bundles,
   serialized session/cookie JSON, `apiKey`/`private_key`, Cloud SQL DSNs, Slack webhooks. The public
   Firebase web config is allow-listed explicitly; anything else new fails.
7. Server-action round trip (sandbox tenant only) does not bounce to `/login` and preserves `?configure=…`.
(Removed: Remote Config ETag — admin-web does not use Remote Config.)

### 2.6 Real-user error diff
Query Grafana/Loki for `app=admin-web` errors in the last 24 h grouped by route; diff vs previous day. This
is the only signal for hydration/history bugs that a synthetic sweep never triggers.

### 2.7 Regression ledger
`tools/sentinel/ledger/REG-NNNN.yaml`: `source_pr`, `hash`, `symptom`, `surface`, `assertion` -> one of the
probes. Seeded from the ~60 hashes cited by the mining agents; supersedes `review_bugs_ledgers.md` (link it).
Entries are written by a script from merged `fix(` PR metadata, never by the healer. Simple guard: CI fails
if an entry is deleted (no `supersedes:` ceremony).

### 2.8 Coverage (self-growing, derived, never hand-maintained)
- Routes: bootstrap nav ∪ page.tsx nightly. Charts/numbers: DOM discovery nightly. Endpoints: HAR nightly.
- New endpoint/field/chart without an oracle -> listed in the report as **candidate**, warn mode. Oracle/fact authoring stays with Ravi + Claude in a supervised session, not the nightly.
- Every merged `fix(` PR -> REG entry the same night.
- `sentinel-coverage-guard` PR gate: **deferred** until coverage is mature (would block every human PR today).
- Report line: `coverage: N routes · N charts · N endpoints · N facts · N candidates`.

---

## 3. Stage 2 — Triage
- Same assertion failing after 1 retry with different evidence -> flake, logged.
- `/readyz`/`/version` failing or > 30 % routes failing -> env down; one escalation message; if still down next night, silent until recovery; no PRs.
- `/version` SHA changed mid-run -> discard, retry once after 10 min.
- Cluster key = (route family, assertion type, file hint from stack/HAR). Same cluster failing 3 consecutive nights -> **quarantine** (report only) until a human clears it in the state file.
- **Heal-eligible** = error boundary, Layer 1 chart<->API, clipping, phone overflow, console/hydration/duplicate-key, ISO-date leak. **Never heal-eligible** = Layer 2/3 data, latency (unless 2-night rule), Lighthouse, persona/permission, anything requiring writes.

## 4. Stage 3 — Heal (OFF for the first 14 nights; report-only until false-positive rate is known)
- `claude -p` in a fresh worktree off `origin/main`, env `GOATOS_AI_SETUP_GUARD=0 GOATOS_GRAPH_GUARD=0 GOATOS_RTK=0`, `git config user.email sentinel@mesha.sg` (identity hook requires `@mesha.sg`), `.agent/scope.json` per cluster (existing `check-write-scope.sh` enforces it), `--max-turns 40`, `--max-budget-usd` per cluster.
- Input: failing assertion, evidence paths, owning REG entry, the "shared root" table so it fixes the wrapper not the feature.
- **Deny list is a prerequisite, not a rule in the prompt.** Today `.claude/settings.json` runs
  `check-write-scope.sh` only on `Write|Edit|MultiEdit` — a `Bash` `sed`/heredoc bypasses it. Before
  Stage 3 is ever enabled: (1) a `PreToolUse` hook on **`Bash` too** that parses the command for write
  targets (redirects, `sed -i`, `tee`, `cp/mv`, `git checkout --`, `python -c` writers) and denies the
  paths below; (2) the healer worktree runs inside an OS sandbox (`sandbox-exec`/bubblewrap or a
  container) where those paths are mounted read-only, so the hook is defence-in-depth, not the only
  wall; (3) post-heal `git diff --name-only` is checked against the same list and any hit rejects the
  attempt. Paths: `tools/sentinel/**`, `apps/admin-web/scripts/**`, `tools/perf/**`, `facts/**`, `ledger/**`, `*.test.*`, `Makefile`, `.claude/**`, lockfiles, and — until §7 structural PRs land — `mesha-theme.css` and `admin-ui-contract.ts` (243-touch conflict magnet).
- Accept only if ALL: probe **red on `origin/main` and green on the fix commit in the same run, same stg** (no red-before = flake, rejected); both viewports; no new failures on that route; diff <= 8 files / <= 300 lines within the cluster's allowlist; assertion-file hashes unchanged; diff free of `try {`, `?? ''`, `display: none`, `return null`, `eslint-disable`, `.skip(`, `.only(`, `hidden`, `opacity: 0` (any hit -> downgraded to "proposal, needs human"); `tools/ci/run-local-ci.sh admin-web` green (10–20 min; full `make ci-local` only before PR).
- Caps per run: max 5 clusters healed, 3 attempts each, 60 min wall-clock for Stage 3, whole run `timeout 3h`.

## 5. Stage 4 — PR
- One PR per night `autofix/YYYY-MM-DD` -> `main`; commits `fix(...)` per cluster, `ledger(...)` (script-generated). Body: findings table, before/after screenshots, latency vs 7-night median, unfixed list with diagnosis.
- Night N+1 never touches PR N. Overlapping-file clusters reported as "blocked by #N". PR open > 7 days -> auto-close with comment; findings re-file fresh.
- State: local JSON on the PC + pasted into each PR body; **no state branch**.
- Merge: human, `make land-main` receipt (AGENTS.md). Never `gh pr merge`.

## 6. Stage 5 — Report + self-observability
- Always one message, even on total failure, with stage reached. Channel pluggable (Slack / WhatsApp /
  email / GitHub issue). **Secret provisioning is explicit:** the runner has no `gcloud`, so the webhook
  is not fetched from Secret Manager at runtime. Ravi provisions it once — either a **new** Slack webhook
  created for this channel or the existing `goatos-stg-deploy-slack-webhook-url` value, entered on
  the PC during `pc-setup.sh`. `pc-setup.sh` ends by sending a test message and the preflight sends a
  "start" ping every night, so a broken notify path shows up as a missing start, not silence at 05:00.
  The dead-man monitor (§ below) is a **second, independent** channel (healthchecks.io-style URL, also
  provisioned in setup) so report and alarm cannot fail together.
- Heartbeat POST at start / each stage / end (duration, exit code) to a cron monitor (healthchecks.io-style URL entered in setup; no GCP dependency) that alarms if no "end" by 05:00 — dead-man's switch for sleeping PC / crashed Chromium / expired login.
- `run.log` + `report.json` per night kept 30 days on disk. Stage 0 preflight fails fast with distinct messages (Claude login, gh, proxy, token mint, Chrome).

---

## 7. One-time structural PRs — **first**, before any heal (otherwise nightly heals are whack-a-mole and conflict with every human branch)
1. `lib/admin-ui-contract.ts`: fail-open (render key name + Faro log) at the 5 `throw` sites.
2. `ChartFrame` wrapper owning empty-state, padding, label-fit for the 6 SVG chart components.
3. `DataTable` owns `overflow-x`; delete the 7 per-feature wrapper copies.
4. Static guards: no timestamps in cache keys; scoped chart CSS class names.

---

## 8. Runner setup (Ravi's PC)
- **Correction from v1:** Claude Code `/schedule` routines run in Anthropic cloud (fresh clone, allow-listed network, no DB proxy, no local secrets) — not usable here. Runner is `cron`/`launchd` -> `tools/sentinel/nightly.sh`; Stages 0–2, 4, 5 are plain `node`/bash; only Stage 3 invokes `claude -p`. (Desktop-app "Local" scheduled task is an alternative only if the Desktop app stays open.)
- Ubuntu or Windows+WSL2 (Ravi's own always-on PC; how he secures it is his call). `pc-setup.sh` installs Node 22, pnpm, Playwright (Chromium + WebKit), Lighthouse, `gh`, Claude Code CLI, optional Cloud SQL proxy, and either installs `code-review-graph`/`repowise`/`rtk` or sets the disable env vars for the repo hooks. Disable sleep.
- Ravi runs once: `claude login`; `gh auth login` (bot account or his own — his choice; the only hard requirement is that it cannot merge to `main`, which branch protection + `land-main` already enforce); `pc-setup.sh` stores the `goatos-sentinel` SA key and DSN (§2.2). No user `gcloud` login needed on the PC.
- Secrets: `~/sentinel/.env` (or keychain if Ravi prefers — either is fine; it is his machine). Probe users: Firebase password accounts per persona created once with `seed-firebase-password-users.mjs` (needs Ravi's gcloud, one-time); the admin/CEO persona has full access to every tenant and park so the sweep sees everything Ravi sees.
- Hosts pinned from Terraform: `dashboard.mesha.sg`, `api.goatos.mesha.sg`.
- `make sentinel-local` runs everything from any laptop for first validation.

## 9. Needed from Ravi (decisions)
1. **Sandbox tenant on stg** for interaction/write probes (none exists; only tenant `…-000000000001`). Without it, probes 2.1b.1–13 that write are skipped.
2. **DB**: create SA `goatos-sentinel@goatos-stg` (cloudsql.client) + key on the PC; apply the `sentinel_ro` role (`pg_read_all_data` — reads everything; read-only only so it cannot corrupt stg).
2b. **Notify secrets**: new Slack webhook for this channel + a healthchecks-style monitor URL, both entered once during `pc-setup.sh`.
3. Four persona users on stg (admin/CEO, verifier, park_head, director).
4. Notification channel.
5. Bot GitHub account (must be `@mesha.sg` for the identity hook).

## 10. Build order
1. §7 structural PRs 1–4 (unblocks safe healing; reduces conflict surface)
2. UI sweep additions: token minting, bootstrap routes, HAR, determinism, assertions, WebKit project, personas; `report.json` schema; `nightly.sh` + preflight + heartbeat + report — **run report-only nightly from here**
3. Interaction probes (2.1b) as sandbox tenant becomes available
4. Latency nightly (HAR -> manifest, 7-night median) + Lighthouse warn-only + auth smoke + Faro diff
5. Layer 1 chart<->API
6. Ledger seed (60 hashes) + delete-guard, both registered in `guardrail-manifest.json`
7. `sentinel_ro` role + proxy wiring on the PC + Layer 2 oracles (8, as SQL files) + Layer 3 fact seed
8. Coverage generators + candidates line
9. Healer sandbox + Bash-aware deny hook + post-diff check (§4) — **gate for step 10**
10. After 14 report-only nights with known false-positive rate: Stage 3 heal for error-boundary + Layer 1 + clipping/overflow clusters only, with §4 mechanics
11. `sentinel-coverage-guard` PR gate — later

## 11. Cadence — everything is nightly
Stage 1a cold latency → UI sweep (all routes × laptop/phone × 4 personas, Chromium + WebKit top-10) →
interaction probes → Layer 1 chart↔API → Layer 2 API↔DB → Layer 3 facts → Stage 1b steady latency →
Lighthouse → auth/Firebase smoke → leak scan → Faro/Loki diff → ledger tripwires → coverage → triage →
(heal) → PR → report + heartbeat. Target wall-clock < 2 h; hard cap 3 h.
**On stg deploy** (add later): re-run 1a + UI sweep for changed routes immediately.
**Once**: §7 structural PRs, `pc-setup.sh`, secrets + SA key, persona users, `sentinel_ro` role, ledger seed.
Nothing is weekly.

## 11b. Ownership statement
This automation runs on the CEO's own machine against the CEO's own company data. It has **full read
access to goatos-stg** (all tables, all tenants, all parks) and full read of the dashboard under the
admin/CEO persona. Earlier drafts proposed PII exclusions, view-only roles, laptop-only or cloud-only DB
paths, key rotation schedules and machine-hardening requirements; Ravi rejected those as not his
constraints, and they are removed. The only invariants the automation keeps are the ones that protect the
system from the automation itself: **read-only DB**, **no deploys**, **no merges to `main`/`stg`**, the
**healer cannot edit its own probes**, and **hard cost/time caps**.

**Read-only, stated once and for all:** the automation never writes to goatos-stg. The DB connection is
`default_transaction_read_only=on` with no DDL; every oracle is a `SELECT`. It reads and validates data,
nothing else. The only place anything is *created* is the app itself via the interaction probes (§2.1b:
save a form, submit twice) — those go through the dashboard UI into a **sandbox tenant**, never through
the DB connection, and they are the one thing Ravi can still switch off by not creating that tenant.

## 12. Explicitly out of scope
Android app parity (many `feat(android)` commits) — web only; say so in every report. i18n copy (no admin-web locales). Pixel-diff baselines. Claude drafting backend oracle SQL unsupervised.
