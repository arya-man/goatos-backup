# Mobile Review — Goat OS Android (native Kotlin + Compose)

The operator + leadership app lives in `apps/goatos-android/` — one common
role-aware native app (Kotlin + Jetpack Compose, app id `sg.mesha.goatos`).
Architecture: `docs/mobile/README.md`. Offline-first law:
`docs/decisions/android-offline-first.md`. Fetch/pagination law:
`docs/decisions/mobile-data-fetch-anti-patterns.md` +
`docs/decisions/mobile-fetch-fix-backlog.md`. Performance/memory:
`docs/mobile/performance-and-memory.md`. Clean-architecture module rules:
`apps/goatos-android/MODULE-MAP.md`.

> **Verify-against-source, not memory.** File paths, DAO/DTO names, screen names,
> and the exact guard offender list below are anchors that drift. Confirm each
> against the committed source it names (`apps/goatos-android/**`, the docs above,
> `tools/agent-hooks/check-mobile-list-fetch.mjs`) at review time. Findings cited
> inline (`MOB-*`, `C35-*`, file:line) are **illustrative examples of the pattern**
> from a prior audit — re-verify the live code before flagging.

## When this lens fires

Load this reference when the change touches `apps/goatos-android/**` — **OR** when
a backend contract / DTO / list-endpoint change reaches a mobile consumer (blast
radius, not diff size; see SKILL "Proportionality & blast radius"). A backend-only
diff that adds a list endpoint with no keyset cursor, drops `next_cursor`, or
changes a response shape the Android client consumes is reviewed HERE too, because
the anti-pattern originates at the contract and `make mobile-guard` is diff-scoped
(it sees no mobile files in a backend-only diff and passes green — it cannot catch
a backend-induced mobile anti-pattern). **The skill review is the only backstop.**

## Module layout (clean architecture — dependency rule)

```
apps/goatos-android/
  app/                 # MainActivity, AppNavHost, boot (Session/Bootstrap VMs), per-screen ViewModels
  feature/             # feature modules (feature-auth, feature-record, …) — Compose UI + VM
  core/
    core-model/        # pure domain models (no Android, no network)
    core-network/      # Retrofit AppApi, DTOs, NetworkModule (adapters — SDK confined here)
    core-database/     # Room DB, entities, DAOs
    core-data/         # repositories: network → Room → Flow (the SSOT seam)
    core-datastore/    # DataStore (SessionStore, DeviceStore)
    core-designsystem/ # theme, ProvideAppLocale, shared Compose
    core-permissions/  # role/authority gating (UX only, not the auth boundary)
  device/              # RFID/scanner hardware adapters
```

Dependency rule (verify in `MODULE-MAP.md`): `feature`/`app` → `core-data` →
`core-network` + `core-database`; UI never talks to Retrofit directly, repositories
never import Compose. A ViewModel reading `AppApi` directly (bypassing a repository
+ Room) is an architecture finding.

## Mobile golden rule — Room is the UI source of truth; backend owns the contract

The Android app is a **renderer**, exactly like admin-web (see `frontend.md`
Golden rule). Backend OpenAPI/app contracts own navigation, labels, page-size
semantics, disabled reasons, empty/error copy, and summary-vs-detail field sets.
On-device, **Room is the single source of truth for every READ screen** — the
screen renders from Room; the network refresh runs in the background and upserts
Room (stale-while-revalidate). This is a hard rule (`android-offline-first.md`).

## 1. Room SSOT / offline-first (screen reads)

Pattern required for every screen-facing read: **network → upsert Room → repo
exposes `Flow` → ViewModel observes Room → UI renders Room.** Refresh-on-open only
upserts; the observed Flow re-emits.

- [ ] **No network-only read repository.** A thin `api.xxx()` pass-through with no
      Room entity/DAO/Flow is BANNED for screen reads (illustrative offenders:
      Tasks `MOB-001`, Roster/Coverage `MOB-007`). New read models ship with their
      Room entity + DAO + Flow from day one.
- [ ] **Offline re-entry renders cache** — never a blank/loading wall when cached
      data exists. Cold start with no network shows the last Room state + a
      stale/offline indicator.
- [ ] **No growing JSON-blob cache.** Do not persist whole response blobs that
      accumulate forever; store per-item rows and read a bounded window. Blob
      caches need TTL + row/byte cap + LRU eviction (illustrative: `C35-017`).
- [ ] **Corrupt/old-schema blob is quarantined, not silent-null.** A
      `runCatching { decodeFromString(...) }.getOrNull()` that maps a bad row to
      `null` yields a permanent false-empty/loading state offline. Version the
      envelope, classify the decode error, delete/quarantine the row, surface a
      real error/stale state (illustrative: `C35-022`).
- [ ] **Error ≠ empty.** A network/decode failure must be a distinct state from
      "no data" — a coverage/exception mapped to `null == hasCoverage=false` lies
      to the user (illustrative: `MOB-007`).
- [ ] **No fixture/sample data in production state.** `sample*State()` /
      `ScreenSamples` values must not seed a production ViewModel's initial or
      null-emission state — cold/offline is exactly when the fake identity/counts
      (e.g. "Gandhi 1", 12/40) never get replaced (illustrative: `MOB-005`).
      Empty/loading/error use zero-data production constructors; samples stay in
      debug/preview/test sources only.

## 2. Pagination — both layers, ~20/page, never bulk

A phone viewport holds ~7-10 rows. Pulling 50/100/200/1000 rows to render is the
mobile twin of compute-on-read. Rules (`mobile-data-fetch-anti-patterns.md`):

- [ ] **No bulk fetch for a screen.** No `limit=50/100/200/1000` for a normal
      list; page size ~20 with infinite scroll (prefetch next at item ~17-18).
      Oversize constants are machine-flagged by `make mobile-guard` (illustrative
      offenders: `ScanViewModel` limit=1000 `C35-006/018`; `GAPS_LIMIT=50`
      `MOB-008`).
- [ ] **Keyset cursor at BOTH layers.** Backend returns `next_cursor` (+ `total`);
      the network fetch AND the Room read the UI observes use the same keyset +
      ~20 page size. `observeAll()` / `SELECT *` / an ever-growing accumulated
      blob just moves the over-fetch from network to DB — BANNED. Room read is a
      bounded keyset window (`PagingSource`; Paging 3 + `RemoteMediator` for large
      lists).
- [ ] **`next_cursor` is consumed.** A DTO that models rows but drops the returned
      cursor, or a screen with no load-more intent, silently stops at page 1 —
      row 201+ never reaches Room/UI and displayed counts are wrong (illustrative:
      L2 execution `MOB-003`, calendar page-2 `MOB-004`, gaps `MOB-008`, scan
      roster transport `C35-006`/FIXCHK-004). Verify the Android DTO models
      `next_cursor`, Retrofit sends a `cursor` query, and the repo keys Room pages
      by cursor/window.
- [ ] **Continuation pages persist to Room**, not just first page. Page 2 held in
      plain ViewModel fields disappears on process death/offline (illustrative:
      `MOB-004`). Never append raw network DTOs into ViewModel state — drive all
      pages from a bounded Room `PagingSource`/`RemoteMediator`.

### L0 → L3 drilldown model (verify the whole chain, not one screen)

- [ ] **L0 overview** (calendar week/month, dashboard) shows **markers/dots/counts
      only** — one per-day marker from a backend day-marker set
      (`includeDateMarkers` / `CalendarDateMarkerDto`). NEVER fetch or parse a
      day's events to draw the grid. (Marker aggregation is deliberately
      independent of the item page — a `limit=1` item page with
      `includeDateMarkers=true` is correct, not a truncation bug.)
- [ ] **L1 day/worklist** — keyset page ~20, infinite scroll.
- [ ] **L2 shed/drive/task detail** — keyset page ~20; **keeps task_id / batch_id /
      shed identity**.
- [ ] **L3 vaccine capture / scan roster** — keyset page ~20; done/pending/skipped
      animals paginate; NEVER the whole cohort.
- [ ] A drive is a **mix of sheds, never grouped by vaccine** — coverage-by-vaccine
      is a metric, not the drive grouping.
- [ ] **Navigation carries exact identity.** Routes/ViewModels/DTOs/outbox thread
      task/SOP-version/batch/shed/row-version end to end. Do NOT "choose the first
      assigned task" after a shed-wide scan — that loses which task the operator is
      executing (illustrative: `C35-006`). Back/refresh/offline transitions
      preserve the selected level and identity.
- [ ] **Empty states distinguish** no-data / not-loaded-yet / offline-no-cache /
      forbidden / backend-error — each a real designed surface, not a blank list.

## 3. Memory, lifecycle & performance

Target device budget is a 2-3 GB low-end phone; scan feedback ≤120 ms, zero
dropped frames (`docs/mobile/performance-and-memory.md`).

- [ ] **No `Context`/`View`/`Activity` held in a ViewModel** — classic leak.
- [ ] **`stateIn(SharingStarted.WhileSubscribed(5_000))` for screen read flows** —
      NOT a forever `collectLatest` bridge in `viewModelScope`. A backgrounded
      back-stack VM that keeps observing Room and rebuilding state burns
      CPU/battery/heap for the whole session (illustrative: `MOB-010`). Genuinely
      hot device streams (RFID) are exempt but must be cancelled on `onCleared`.
- [ ] **Parse/decode/map ONCE, off Main.** Move `decodeFromString`/DTO mapping to
      `flowOn(Dispatchers.Default)` in the repository — decoding a large blob on
      the Main collector context janks scan/scroll and risks ANR (illustrative:
      Calendar/Execution `MOB-009`, decode-on-Main `C35`). Never re-parse a field
      inside `.find`/`.filter` (O(n²)).
- [ ] **Outbox is bounded and pruned.** No `SELECT * ... ORDER BY` without LIMIT
      observed in application scope forever; prune SUCCEEDED terminal rows under a
      retention policy; keep failed/conflict work durable; the UI observes counts +
      a bounded recent window, not the whole historical payload table
      (illustrative: `MOB-006`).
- [ ] **RFID/scan is capped and O(1).** Bounded visible feed (no unbounded
      prepend-forever), map RFID→row in O(1) (indexed), durable aggregate
      counters — not a linear search/copy per scan on Main (illustrative:
      `C35-018`).
- [ ] **Lazy lists use stable keys + `contentType`.** Dynamic `items()`/
      `itemsIndexed()` without a stable backend/device id identifies rows by
      position — extra recomposition and wrong-row remembered state on
      insert/reorder (illustrative: `MOB-011`). No eager `Column { list.forEach }`
      for a data list.
- [ ] **UI collection is lifecycle-aware** — `collectAsStateWithLifecycle()`, not
      plain `collectAsState()`, for screen state.

## 4. Cross-principal isolation & sign-out (P0 class)

A shared/reassigned device or role switch must not leak the prior principal's data
(illustrative: `C35-001`, a P0 tenant/security boundary break).

- [ ] **Sign-out is a wipe-all clean slate**, routed through one fail-closed
      coordinator: while the old bearer is still valid, stop/cancel sync +
      WorkManager jobs, best-effort/idempotently deregister the backend device/FCM
      binding (`POST /app/devices/{device_id}/deregister`), then transactionally
      wipe **every** app-owned surface — both Room DBs (including unsynced outbox
      rows per the wipe-all policy), all DataStore keys (language, install/device
      ids), SharedPreferences, WorkManager unique jobs, pending capture/upload
      files + caches, SavedState/drafts, and in-memory singleton/bootstrap/nav
      state — before Login is shown. Firebase/credential sign-out is last.
- [ ] **Every persisted row is principal-scoped** as defense in depth (cache keys
      namespaced by tenant/principal/role/authority revision, not filter-only) —
      but scoping is not an excuse to retain anything after logout.
- [ ] **A new persistence/job type registers in the wipe inventory** — a new Room
      entity, DataStore key, SharedPreferences file, WorkManager job, or cache dir
      added without wipe registration is a finding (the destructive-logout
      certification test must fail on it).

## 5. API contract back-compat

- [ ] **Android contract changes are backward-compatible or explicitly versioned.**
      A field removal/rename/type change on a DTO the shipped app decodes can crash
      or silently drop data on older installs. Verify the OpenAPI/app contract and
      the generated/hand-written Android DTO stay aligned, and that a list endpoint
      the app consumes carries keyset `next_cursor` + `total` so the client is not
      forced to over-fetch (contract-origin anti-pattern — flag at the source PR).

## Guards (necessary, not sufficient)

- `make mobile-guard` (`tools/agent-hooks/check-mobile-list-fetch.mjs`) blocks
  over-fetch/`SELECT *`/missing-key patterns. **Diff-scoped in CI** — a commit with
  no mobile files passes instantly, so it is BLIND to a backend-contract-induced
  mobile anti-pattern. Run `node tools/agent-hooks/check-mobile-list-fetch.mjs
  --all` for a whole-tree pass; the skill review is the backstop the diff-scoped
  guard cannot be.
- Android compile/test is NOT on the ordinary-PR gate (illustrative: `C35-008`) —
  a Gradle-green claim needs the build to have actually run (local Gradle needs a
  JDK; absence is a verification limitation, not proof). Do not accept "compiles"
  without evidence it was built.

## Mobile review checklist

- [ ] Screen reads are network → Room → Flow → UI; no network-only read repo
- [ ] Offline re-entry renders cached Room data; no blank/loading wall
- [ ] No blob cache without TTL/cap/eviction; corrupt/old-schema rows quarantined, not silent-null
- [ ] Error/empty/loading/offline/forbidden are distinct real states; no fixture/sample data in prod state
- [ ] No bulk fetch (`limit` 50/100/200/1000) for a screen; page ~20 keyset
- [ ] Keyset cursor at BOTH layers; `next_cursor` consumed; continuation pages persist to Room (Paging 3 + `RemoteMediator` + `PagingSource` for large lists)
- [ ] L0 = day markers only (never fetch day events); L1/L2/L3 each paginate ~20 and carry task/shed/batch identity; drive = mix of sheds
- [ ] Navigation threads exact task/SOP-version/batch/shed/row-version; no "choose first task" after scan; back/offline preserve identity
- [ ] `stateIn(WhileSubscribed(5_000))` for screen flows — no forever collectors; hot device flows cancelled on clear
- [ ] Parse/map ONCE, off Main via `flowOn(Dispatchers.Default)`; no O(n²) re-parse
- [ ] Outbox bounded + terminal rows pruned; UI observes counts + bounded recent window, not the whole table
- [ ] RFID/scan capped buffer + O(1) tag match; no unbounded feed growth
- [ ] Lazy lists have stable keys + contentType; UI uses `collectAsStateWithLifecycle`
- [ ] No Context/View/Activity in ViewModels
- [ ] Sign-out wipes ALL app-owned state through one coordinator + deregisters device/FCM; caches principal-scoped; new persistence registers in the wipe inventory
- [ ] Android contract changes are back-compat or versioned; consumed list endpoints carry keyset cursor
- [ ] `make mobile-guard` run; whole-tree `--all` for a real pass (diff-scoped CI is blind to backend-induced anti-patterns); Android build actually ran if compile is claimed

## Motion & transitions lens

Contract: `docs/mobile/transitions-and-motion.md`. Fires when a change touches
Compose navigation transitions (`AppNavHost`), `AnimatedContent`/`Crossfade`, or a
`ModalBottomSheet`. Motion is a spatial signal, so a wrong transition is a real
finding, not a nit.

- [ ] **Drill navigation slides (shared axis X), not fades** — deeper navigation
      uses `slideIntoContainer(Start)`; Back reverses via `pop*` (`End`). Flag a
      faded drill or a missing/asymmetric `popEnter`/`popExit`.
- [ ] **Top-level tab switches fade through, not slide** — bottom-nav peers
      (Calendar/Overview/Alerts/You) must not inherit the drill slide (invents a
      false forward/back order). Flag a peer swap that slides. This is the known
      standing gap — re-verify `AppNavHost` at review time.
- [ ] **Contextual surfaces use `ModalBottomSheet`** — no hand-rolled slide-up
      offset/`animateFloatAsState` sheet substitutes.
- [ ] **No forced shared axis Y/Z** where the relationship doesn't call for it;
      no decorative animation that contradicts the spatial model.
- [ ] Polish (note, don't block): Emphasized easing over `tween` default;
      predictive-back opt-in.
