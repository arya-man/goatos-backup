# Android / Mobile Agent Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Android Proof Media Rule

For Goat OS Android proof capture/preview work, never add a client-wide
proof/video cap. Caps must be explicit per feature and field; a vaccine rule
must not affect weighing, PC Care, feed, counts, health, toxin, workflow, or any
other proof flow.

Post-capture image/video proof cards should use the shared `ProofMediaPreview`
surface with play/pause, fullscreen, share, retry/re-record where applicable,
and feature-owned `onPreviewAction` analytics. Track enough bounded context to
trace screen -> field -> proof row -> outbox -> retry -> backend registration ->
submit -> success/failure. Do not show raw internal identifiers such as
`feed_water_removal` as user-facing titles.

Every `ProofMediaPreview` caller must pass a stable `mediaIdentity` based on the
proof id, server proof id, outbox item id, feature slot id, or attachment id. Do
not key preview/player state by temporary signed GCS URLs. A remote proof photo
on an opened/detail proof surface may render on first paint when it is bounded to
that visible item, cached by stable `mediaIdentity`, attributed through
`onPreviewAction`, and annotated for the egress guard. Hidden/off-viewport list
media must not auto-download or prefetch, and remote video bytes must not move,
auto-prepare, or poster-probe until explicit play/open/share intent. Any direct
remote attachment download must be tap-triggered, bounded, cached locally, and
explicitly annotated for the egress guard.

For any proof-backed business workflow, proof upload success is not final
business success. Green/done user-facing states must wait for the feature's
business write or server read model: scan capture, animal observation, feed
completion, PC Care slot/task-proof registration, task submit, or equivalent.
If a link/register/submit fails after a blob upload succeeds, retry that small
business write with the existing proof id; do not re-upload media just to repair
the link. See `docs/decisions/proof-business-ack-contract.md`.

When Ravi asks for judge/subagent validation and spawning fails due to agent
capacity, close completed or old non-critical agents and retry immediately.
Do not stop on agent-capacity while stale agents can be safely closed.

For every new Goat OS task, start from a clean checkout of the latest
`origin/main` unless Ravi explicitly names an existing branch, PR, worktree, or
dirty local state as the target. Do not begin new work from whatever branch the
terminal happens to be on. If the canonical checkout is dirty or stale, create a
fresh isolated worktree from `origin/main` and do the task there; only inspect an
old branch after the task specifically requires that branch.

Whenever the maintainer says `review` for any Goat OS code, branch, or PR,
Codex, Claude, and any other coding agent must use the same review lens: inspect
the last one month of relevant commits for regressions, repeated patterns, and
context drift, and review the change against the current sync architecture
rather than only the visible diff. Treat mismatches between the PR and current
architecture, contracts, operational read models, mobile/backend/admin sync, or
shared kernel flow as review findings even when the diff compiles.

For every Goat OS review or change that touches backend, admin-web/frontend,
Android/mobile, OpenAPI, migrations, seeds, or any read model with shed, pen,
partition, operational location, work-board, weighing, vaccination, feed, counts,
verification, task, or command-board labels, the review lens must explicitly
check operational-location display identity. First inspect the last one month of
relevant commits for repeated shed/pen/partition naming fixes, then verify the
candidate does not reintroduce either direction of the bug:

- missing partition identity: `Castro` when the work is in `Castro 1` or
  `Castro 2`; `Godel 2` when the work is in `Godel 2 - Part 1`; `Mandela 1`
  when the work is in `Mandela 1 - Part 10`.
- doubled partition identity: `Castro 1 1`, `Castro 2 2`,
  `Godel 2 - Part 1 - Part 1`, `Mandela 1 - Part 10 - Part 10`.

The accepted visible forms are concrete operational sheds such as `Castro 1`,
`Castro 2`, `Mandela 1 - Part 10`, and `Godel 2 - Part 1`. A physical shed name
plus partition label may use the canonical composer. An already-composed
display string must never be passed back into that unconditional composer; use
the composed-name-safe helper or the backend-owned `operational_location_display`
field. Private suffix guards like `endsWith(partitionLabel)` are review
findings because they only patch one surface and let backend, frontend, or mobile
repeat the same bug.

For any handwritten PostgreSQL change, treat the final SQL and final bind
arguments as one contract. Prefer sqlc or `pgx.StrictNamedArgs`; dynamically
assembled or pruned SQL must use the shared bound-query validator and executable
PostgreSQL coverage for unresolved production shapes. Additions, removals,
reordering, and pruning must pass `make postgres-bind-contract-guard`; never
substitute a hardcoded current maximum placeholder or broaden a legacy baseline.

For any review or code change that touches admin-web, website, dashboard,
frontend, CSS, page contracts, route definitions, or web-visible copy, the
review lens must include laptop and mobile UI/UX. Do not stop at compile,
typecheck, or a single desktop screenshot. Verify the affected route and every
route-owned nested tab/state, including page tabs, left/right sidebars,
drawers/modals/popovers, dynamic detail pages, charts, tables, and horizontal
scroll regions. Run or require the local admin-web responsive visual guard
(`npm --prefix apps/admin-web run responsive:guard`, or the current equivalent)
against both laptop and mobile viewports, inspect the generated screenshots
before presenting them as proof, and treat missed route/tab/drawer coverage in
the guard itself as a review finding.

Capturing that guard is not the same as reading it. Every run writes an
annotated `-issues.png` (findings boxed in red) beside the plain screenshot,
plus `route_failed=` lines; open and read both at **laptop 1440 and phone 390**
before calling a UI change proven. `apps/admin-web/scripts/lib/regression-checks.mjs`
is the catalogue — text overlap and cut-off, chart labels collapsed to ~0px or
clipped or colliding or mostly ellipsised, bars with no value, chart frames with
neither bars nor empty-state copy, SVG text clipped/colliding/under the 8px
floor, container and horizontal-page overflow, table cells painting over the
next column, crushed chips, and raw values/codes/contract keys/ISO dates leaking
into the UI. Each is a finding at either viewport, and phone-390 is where most
of them actually appear. Android/Compose screens are not covered by that guard
and their screenshots are OFF by default in `ci-local`: prove them with
`make ci-local-screenshots` and read them the same way.

A guard verdict is evidence, not truth: these rules read the live DOM, so markup
they do not model answers confidently wrong in both directions. Verified example
(2026-09-23): `A-chart-empty-frame` reported `/feed/analytics`'s *Feed mix* card
as having no visible bars while it painted 8 at both viewports, because
`SvgBars` marks its `<svg aria-hidden="true">` and `hidden()` treats `aria-hidden`
anywhere up the tree as not-painted — so that rule cries wolf on every populated
`SvgBars` chart and can never catch a genuinely empty one. Confirm a surprising
verdict against the DOM before filing or dismissing it, and treat a false
positive or blind spot in the guard as a finding to fix in the same change.

This applies identically to Codex, Claude, and any other coding agent, during
development and during review — not at merge time. See
`.agents/skills/goatos-build/SKILL.md` and
`.agents/skills/goatos-code-review/SKILL.md`, which carry the same rule.

For Android/mobile/backend reviews that touch camera, proof media, attachments,
signed URLs, uploads, previews, player screens, or billing/infra, include
post-upload media egress risk in the review. Treat opened/detail proof-photo
preview as intended when it is visible, bounded, stable-identity cached,
attributed, and guard-annotated. Check for hidden/off-viewport list auto-fetch,
remote video auto-prepare or poster/metadata probes, raw
`URL.openStream`/Coil/Media3 downloads outside the guarded proof-media path,
retry loops around expired signed URLs, UI keyed by temporary signed URL instead
of stable proof/slot/attachment identity, missing `ProofMediaPreview.mediaIdentity`,
missing preview analytics, missing backend download attribution, and missing
Cloud Monitoring/Slack billing alerts.

This review lens is not satisfied by reading only the PR diff. The reviewer must
run or inspect `tools/agent-hooks/check-android-proof-media-egress.mjs --all`,
then manually enumerate every changed or adjacent proof-media consumer:
`ProofMediaPreview` wrappers, raw Media3 players, `MediaPlayer`, Coil/AsyncImage,
`MediaMetadataRetriever`, OkHttp, raw `URL` streams, backend list/read endpoints
that mint `download_url`, and admin-web media tags. A review that says "all good"
without naming these surfaces is incomplete. For PRs caused by a billing spike,
the PR description must list: root cause, every newly found miss, files fixed,
guards/tests run, and which alerts are live versus only configured in code.

A review request alone is not permission to push directly to `main`. If the
maintainer explicitly asks to push, land, or merge after review, continue
through the hard local CI/landing receipt gate before touching `main`.

Any backend-composed navigation key or user-facing category added in the
backend must have an explicit Android/frontend display mapping before it ships:
icon, label/copy behavior, and layout fit. Never let mobile or web silently fall
back to a generic icon, raw enum/title, or unbounded label just because the
backend added a new key.

For phone E2E, test only the Android user profile and app package visible to
the maintainer unless they explicitly authorize switching.

For any local/OCI throwaway feature-testing seed, always seed and verify
operator-visible work at operational partition identity: shed + concrete
partition label, for example `Godel 1 - Part 1`. Do not seed feature-test cards
that render as a bare shed name such as `Godel 1`, and do not use `whole` as the
visible partition label in phone evidence unless the maintainer explicitly asks
for a whole-shed case.

Before giving Ravi any screenshot, screen recording, or image as proof of a UI
state, Codex/Claude must validate the artifact visually first: open the captured
file, confirm it shows the requested target screen/state and not login,
loading, an error page, stale content, or the wrong route, and only then present
it. If the artifact cannot be visually validated, do not present it as proof;
say exactly what blocked validation and recapture or ask for the missing auth.

## Android CLI Bootstrap - Claude AND Codex

Before any Goat OS Android developer command, Claude, Codex, and human
developers must ensure Google's Android CLI is available. Use the repo helper;
do not hand-roll separate install steps:

```bash
bash tools/dev/ensure-android-cli.sh
```

The helper is idempotent. If `android` is missing, it installs the user-local
Android CLI for the developer's platform, runs `android update`, runs
`android init`, and runs `android skills add --all` so Codex, Claude, and other
detected agents receive the official Android skills. If `android` is already on
`PATH`, the helper stays quiet unless the base Codex/Claude Android CLI skill or
the broader official skill set is missing. The Android entrypoints
`make android-doctor`, `make android-emulator-ensure`, and `make
android-dev-run` already run this first; agents that call lower-level Android
scripts directly must preserve that bootstrap.

For what Android CLI and Journeys are allowed to prove in Goat OS, read
`docs/mobile/android-cli-and-journeys.md`. Journeys supplement the existing
Gradle/Paparazzi/phone-QA gates; they do not replace them.

**HARD RULE — phone/mobile QA must NEVER use or repoint the default ports.**
`127.0.0.1:3300` (admin-web), `127.0.0.1:8080` (API), and `127.0.0.1:5433`
(database) carry the maintainer's LOCAL REPLICA OF STG DATA. Phone QA is mock
scan data (the throwaway 20-animal seed). Never start, stop, kill, restart, or
repoint anything on `3300`, `8080`, or `5433` for mobile testing, and never free
a default port by killing whatever holds it — parallel agent sessions (Claude
and Codex) share this laptop, and the process you kill is another session's
stack.

Run the phone-QA API on a NON-DEFAULT port and remap the tunnel instead. The
device always calls its own `localhost:8080`, so only the host side moves — no
APK rebuild and no token re-mint are required:

```bash
# phone-QA API on 8081 -> throwaway DB 127.0.0.1:15544
# set GOATOS_HTTP_ADDR=127.0.0.1:8081 for that backend process
adb -s <serial> reverse tcp:8080 tcp:8081
```

Before telling the maintainer a physical-phone run is clean, verify this exact
single-target chain and write the result in the handoff:

```bash
lsof -nP -iTCP:8081 -sTCP:LISTEN
ps eww -p <8081-pid> | tr ' ' '\n' | rg 'GOATOS_HTTP_ADDR|DATABASE_URL'
adb -s <serial> reverse --list
```

Expected for phone QA:

```text
host API: 127.0.0.1:8081
host DB:  postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable
device:   tcp:8080 -> host tcp:8081
```

If any of those three do not match, stop and fix the target before scanning.
Do not debug vaccine counts, disappeared rows, submit state, or proof upload
state until this target chain is proven. A mismatch means the phone and psql are
looking at different worlds.

Before a fresh vaccination phone test, clear both durable sides before launch:
backend vaccination scan/proof/submission rows in the throwaway DB, and Android
Room/app data for the visible profile. Then install, set `adb reverse`, launch,
and screenshot-check the actual phone. Do not launch first and clear later. A
clean test starts from no `sop_task_scan_*`, no vaccination completion/rejection,
no proof artifact/outbox rows for the prior run, and an app profile cleared via
the visible Android user.

For Android physical-device work, the target-chain proof is not enough. Every
time an agent installs, clears, launches, relaunches, changes `adb reverse`,
changes backend process/port, changes DB seed/reset state, or asks the
maintainer to test, the agent must also verify the actual opened phone screen is
showing data from the throwaway DB. Use a real device screenshot or focused UI
inspection after launch, and compare the visible shed/task/counts/tags against a
fresh SQL read from `127.0.0.1:15544`. Do not stop after "installed" or "DB
cleared"; the handoff is only valid after the phone is open, focused, and
showing the expected throwaway data.

This supersedes any earlier wording suggesting the backend behind `8080` may
swap its database target. Taking `8080` for phone QA caused a real incident
(2026-08-03): the maintainer's `5433`-backed API was killed to free the port,
`8080` was pointed at the throwaway `15544` database, and admin-web then showed
the 20-animal mock set in place of the 324 CPT adults — while the phone's
`adb reverse` still aimed at `8080`, one port flip away from writing mobile scan
data into the stg replica.

The fixture intentionally maps five physical vaccination RFIDs into ten goat
identities across CBE and CPT while preserving the production uniqueness rule on
`goat_identifiers`; Weighing remains free-flow and must keep raw RFID input.

## Mandatory Android APK Source Traceability

Every Android APK uploaded to Firebase App Distribution must be traceable to
the exact source revision that produced it.

- Use only `:app:appDistributionUploadProdRelease` for the active
  production-facing Firebase App Distribution Android uploads. Do not upload
  ad-hoc APK files manually from the Firebase
  console, `firebase appdistribution:distribute`, or any other path unless the
  maintainer explicitly asks for a one-off rescue build and the release notes
  still record the source label.
- The distributed APK must include/bake the source commit and tag metadata
  (`SOURCE_COMMIT`, `SOURCE_TAG`, `SOURCE_BRANCH`, `SOURCE_LABEL`, and the
  matching string resources) and the Firebase release notes must carry the same
  source label.
- Never upload from a dirty worktree. The only exception is an explicit
  throwaway/debug build using `-PallowDirtyFirebaseDistribution=true`; label it
  as throwaway in the release notes and do not use it to answer whether a
  production-like phone APK contains a feature.
- When answering "does the latest Firebase APK have feature X?", first verify
  and record the installed/Firebase release source label, then compare that
  commit/tag against the commit that introduced the feature. If the source label
  cannot be verified, say that clearly instead of inferring from local `HEAD`,
  `origin/main`, or memory.

## Mobile/API Provenance For "Who Did What"

When the maintainer asks who performed an action, whether an old APK caused a
bad submission, which phone captured a bad audio/video proof, or which device
sent an RFID/weighing/vaccination action, check backend audit and request
provenance first.

Android sends these headers on every API request:

```text
X-GoatOS-App-Version
X-GoatOS-App-Version-Code
X-GoatOS-Build-Type
X-GoatOS-Device-Id
X-GoatOS-Platform
X-GoatOS-OS-Version
X-GoatOS-SDK-Version
X-GoatOS-Device-Model
```

Backend request logs include the same fields, and `audit_log.metadata->'client'`
is the durable SQL source. For a specific weighing/vaccination/proof issue,
join from the domain row to its audit event and inspect that client block before
guessing from screenshots, Firebase, or local code. Firebase Analytics is useful
for dashboards; the audit row is the source of truth for a submitted business
action.

- Treat every Android READ screen as offline-first with Room as the single source
  of truth for the UI (hard rule — Claude, Codex, and humans). Backend owns the data;
  on-device, the screen renders from Room and the network refresh runs in the
  background (stale-while-revalidate): persist every backend read response to Room,
  have the repository expose a `Flow` the ViewModel observes, refresh-on-open to upsert
  Room (which re-emits), and show a sync/stale indicator — NEVER a blank/loading wall
  on re-entry when cached data exists. A network-only read repository (a thin
  `api.xxx()` pass-through with no Room persistence) is BANNED for screen-facing reads;
  new read models ship with their Room entity + DAO + Flow from day one. Do not call
  the app "offline-first" until the read models are cached (bootstrap + the write
  outbox already are; Calendar/Control-Tower/Execution/Adherence/Insights must be
  migrated). Full rule + the NetworkBoundResource pattern:
  `docs/decisions/android-offline-first.md`; refs the Android data-layer + offline-first
  architecture guides.
- Every Android READ screen must be refresh-on-open (hard rule — Claude, Codex,
  humans): call the shared `sg.mesha.goatos.core.ui.RefreshOnResume { onEvent(XEvent.Refresh) }`
  composable (`core/core-ui/.../RefreshOnResume.kt`, wraps
  `LifecycleEventEffect(Lifecycle.Event.ON_RESUME)`) near the top of the screen's
  composable body so cached Room data shows instantly and a background refresh
  fires automatically every time the user lands on or returns to the screen — a
  retained ViewModel on the nav backstack must never show data that was only
  fetched once at ViewModel creation. Never rely on a manual sync button/icon as
  the only way to see fresh data; a visible sync affordance is allowed as a
  supplementary manual trigger, not the primary refresh path. Skip this only for
  screens where a resume-triggered refresh would disrupt in-progress user input
  (scan-capture flows, forms, mid-entry screens) — the ViewModel's `refresh()`
  itself must stay non-blocking (upsert Room on success, leave cache visible on
  failure) so this never produces a loading wall. See
  `docs/decisions/android-offline-first.md`.
- Every Android READ screen's sync/refresh icon must show a spinning animation while a refresh
  is in flight and become non-clickable to prevent duplicate refresh triggers (hard rule —
  Claude, Codex, humans). Use the shared `SyncIconButton` composable
  (`sg.mesha.goatos.core.ui.SyncIconButton`, `core/core-ui/.../SyncIconButton.kt`) on every
  screen with a manual refresh affordance: pass `isSyncing = state.isRefreshing` (or
  `refreshInFlight` if the screen names it differently) and `onSync = { onEvent(XEvent.Refresh)
  }`. When `isSyncing` is true, the icon continuously rotates 360° and the button is disabled,
  so duplicate taps are ignored. When false, the icon is static and clickable. Applies to all
  read screens with visible refresh buttons: Calendar, Sheds, Counts, Approval, Verify (queue),
  Leadership (all three screens), and any future read screens that expose manual sync. This is
  consistent across the app and prevents race conditions from overlapping refresh requests.
- Treat every Android Room schema change as an installed-APK upgrade contract, never just a
  fresh-install schema (hard rule — Claude, Codex, humans). Room builds a DB two ways: a fresh
  install runs `createAllTables` (every @Entity), but an in-place upgrade runs ONLY the registered
  `Migration` objects and then validates against the @Entity set — so an @Entity added to a
  @Database with no migration to CREATE its table compiles, works on fresh installs, and CRASHES
  every upgrade on open (`Migration didn't properly handle <table>`). This actually shipped
  (roster_timetable_cache / roster_coverage_cache, MOB-007) and a plain in-memory Room test is
  blind to it. Required: `exportSchema = true` + committed `schemas/<db>/<version>.json`; every
  version bump ships its `Migration(N-1, N)` that creates exactly the new tables/columns/indices;
  additive + non-destructive (no `fallbackToDestructiveMigration` — the outbox holds unsynced
  operator writes, the cache is the offline SSOT); and BOTH a schema-equivalence `*MigrationTest`
  AND an upgrade-crash `*UpgradeCrashTest` (seed an old-version file via a test-only old @Database,
  reopen with current schema + real migrations, assert no crash + data preserved). Machine-blocked
  by `make room-migration-guard` (`tools/agent-hooks/check-room-migration-safety.mjs`, diff-scoped,
  in the CI `guardrails` job). Full rule: `docs/decisions/room-migration-safety.md`.
- NEVER fetch more than one screen-page of rows on mobile/web (hard rule — Claude,
  Codex, humans). A phone viewport holds ~7-10 items; pulling 50/200/1000 rows to
  render is the mobile twin of compute-on-read. Machine-blocked by `make mobile-guard`
  (`tools/agent-hooks/check-mobile-list-fetch.mjs`, diff-scoped in CI so a commit with
  no mobile code passes instantly); rule + rationale in
  `docs/decisions/mobile-data-fetch-anti-patterns.md`. The rules:
  - **Calendar week/month overview = DOTS ONLY** — one per-day marker (a drive exists;
    optional tone) from a backend day-marker set (`includeDateMarkers` /
    `CalendarDateMarkerDto`). Never fetch or parse a day's events to draw the grid.
  - **Every drill level paginates** — L1 day list, L2 sheds, L3 vaccine-capture
    (done/pending/skipped animals) are each a keyset page of **~20** with infinite
    scroll (prefetch next at item ~17-18). Never request > ~20 rows in one page,
    and never show a tappable "Load more" row/button for normal mobile work
    queues. Pagination is app-owned viewport behavior; users should see only the
    work list plus a passive loading footer while the next page is already in flight.
  - **A vaccination drive is a park visit with a mix of SHEDS, never grouped by
    vaccine** — one drive can contain one or many sheds. Coverage-by-vaccine is a
    metric, not the drive grouping.
  - **Vaccination date moves/reverts are kernel writes, not read-model sidecars** —
    moving a vaccine from a mixed operator-cap drive must update raw
    `vaccination_drive_assignments` membership so the old date loses only that
    vaccine and the target date gains it. Reverting by selecting the original
    date must cancel the active override and restore the original raw
    assignment membership. Never declare this fixed from frontend banners or
    read-time `COALESCE(override_date)` behavior; E2E must assert raw DB rows
    across move and revert while preserving all clinical rule outputs
    (kid/adult, boosters, live/killed spacing, sick/ICU/pregnant/dead/cull
    deferrals) and operator animal caps.
  - Parse/transform each field ONCE (never re-parse inside `.find`/`.filter` → O(n^2)),
    off the Main thread (`Dispatchers.Default`, ideally in the repo via `flowOn`).
  - **Room is the single source of truth, so pagination binds BOTH layers** — the network
    fetch AND the Room read the UI observes use the same keyset + ~20 page size. NEVER
    `SELECT *` / `observeAll()` / an ever-growing accumulated blob; the observed read is a
    bounded keyset window (Room `PagingSource`; Paging 3 + `RemoteMediator` for large lists).
    Otherwise the over-fetch just moves from network to DB.
  If a case is genuinely bounded (e.g. a fixed 7-cell week loop) annotate the line
  `// mobile-guard:ignore: <reason>`; do not disable the guard.
  Android navigation has a separate structural invariant: backend-composed root
  destinations are L0 and alone own the bottom bar/drawer. Every L1/L2/L3/L4
  drill is a distinct hosted `NavHost` destination with Up/Back and no root
  chrome; exact route membership is mandatory, prefix matching and reusing an
  L0 route as a drill target are forbidden, and structural details must not be
  disguised as modal sheets. Machine-blocked by
  `make android-navigation-stack-guard`; canonical decision:
  `docs/decisions/android-navigation-stack.md`.
  Placement is a separate invariant: a FEATURE ENTRY POINT (alerts, inbox,
  videos, profile, a module switch) belongs in the bottom bar or the module
  drawer and NEVER in the top-right app bar, which carries only actions on the
  current screen. Machine-blocked by `make nav-entry-point-placement-guard`;
  canonical decision: `docs/decisions/nav-entry-point-placement.md`.
  The retention twin is memory, not fetch size: an in-heap cache/accumulator that
  grows with no cap/TTL/eviction, or a DAO reading a whole table into memory
  (`observeAll` `SELECT *`), OOMs the phone at scale (fixed in `7058fff2` +
  `d58acac2`). Machine-blocked by `make android-bounded-memory-guard`
  (`tools/agent-hooks/check-android-bounded-memory.mjs`, diff-scoped) — use an
  `LruCache` or a Room `JsonBlobCacheDao` with `readCachedJson` (TTL) +
  `enforceCacheBounds` (row/byte cap), or filter the DAO read to active rows
  (`WHERE status IN (...)`) / a `LIMIT` window.
