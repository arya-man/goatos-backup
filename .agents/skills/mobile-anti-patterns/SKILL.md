---
name: mobile-anti-patterns
description: >-
  Use when building, debugging, diagnosing, or reviewing Android/mobile code under apps/goatos-android/**
  (screens, ViewModels, repositories, Room DAOs, Flows). Enforces Room-SSOT
  offline-first, ~20-row keyset pagination, bounded memory, off-main decode,
  lifecycle-aware collection, stable list keys, and a correct hosted navigation
  stack with L0-only global chrome. Invoke before touching any list/VM/repo/DAO/
  screen/route and before pushing. Complements `make mobile-guard`,
  `make android-bounded-memory-guard`, and
  `make android-navigation-stack-guard` with the how-to-fix.
---

# Mobile anti-patterns (phone-scale + offline-first)

Canonical rulebooks: `docs/decisions/mobile-data-fetch-anti-patterns.md` +
`docs/decisions/android-offline-first.md`. For any screen, navigation, camera, layout,
accessibility, or screenshot change, also read `docs/mobile/android-ui-quality.md` and run
`node tools/agent-hooks/check-android-ui-foundations.mjs`.

## UI foundation (machine: `check-android-ui-foundations.mjs`)
- Derive the cold-start root from backend-visible navigation and test process recreation.
- Treat camera/reader/observer ownership as lifecycle resources: pair every bind/start with
  release/stop on route disposal and backgrounding. Camera proof is an exclusive full-screen UI.
- Prefer Material controls. Custom controls require a 48x48dp target, meaningful semantics, and
  non-color state cues. Never use a tiny clickable `Text` or icon.
- Keep peer geometry stable and use design-system color/type/shape/spacing tokens. Verify compact
  and expanded widths with Paparazzi, then exercise system navigation and capture on a device.

## Fetch / pagination (machine: `make mobile-guard`)
A phone viewport holds ~7–10 items; pulling 50/200/1000 is the mobile twin of
compute-on-read.
- **Never fetch > ~20 rows/screen.** Every drill level is a keyset page of ~20 with
  infinite scroll (prefetch at item ~17–18). No `limit=1000`.
- **Calendar overview = DOTS ONLY** — per-day markers from a backend marker set;
  never fetch/parse a day's events to draw the grid.
- **A drive is a mix of SHEDS, never grouped by vaccine** (coverage-by-vaccine is a
  metric, not the grouping).
- **Partition display:** when a shed has partitions (`Castro 1` + `Castro 2`),
  render the partition label, never collapse into parent. See
  [`docs/decisions/operational-location-display-contract.md`](../../../docs/decisions/operational-location-display-contract.md).
- Genuinely bounded (e.g. fixed 7-cell week loop) → `// mobile-guard:ignore: <reason>`.

## Compose lazy-list keys (machine: `make android-compose-lists-guard`)
The `LazyColumn`/`LazyRow` `key` is the identity of the RENDERED ROW, not of the
domain object it shows.
- **Never key a per-row list by a per-ENTITY id.** The scan roster is one row per
  **obligation**, so `key = { it.goatId }` put a two-vaccine goat (ET+TT · PPR) on
  two rows with the same key → `IllegalArgumentException: Key "<x>" was already used`
  in the LazyList measure pass → whole screen crashes/pops (shipped 0.1.6-stg, fixed
  a9c35a1d). Key the unique per-row id (`obligationId`) or a composite
  `key = { "${it.goatId}|${it.vaccineLabel}" }`.
- **Always supply a `key`** on `items(<collection>)`/`itemsIndexed(<collection>)`;
  positional keys reuse remembered row state (checkbox/expand/scroll) on
  insert/reorder and can crash. The `items(<Int>)` count overload is exempt.
- Bounded exception → `// compose-guard:ignore: <reason>`.
- Also watch (review, not yet machine-checked): missing `contentType` on
  heterogeneous lists, `mutableStateOf` without `remember`, and unstable inline
  lambdas passed per item.
- **Machine-checked (`nested-scroll-in-lazy-items`):** a `Modifier.verticalScroll`
  Column/Row, or another `LazyColumn`/`LazyRow`, nested directly inside a list's
  `items()` row lambda — two scrollables on one axis (infinite-constraint /
  double-scroll bug). Hoist the inner list to its own destination/sheet.

## Phone-scale UI (machine: `make android-compose-lists-guard`; rulebook: `apps/goatos-android/docs/phone-scale-ui.md`)
Real park cardinality is ~100 sheds x ~70-90 animals/shed (~7-8k rows/park). Three shipped
recurrences of the same class:
- **Unbounded rendering.** `<state-or-domain>.forEach { ... Composable ... }` inside a scrollable
  Column/Row instead of a windowed `LazyColumn`/`LazyRow` with `items(..., key = ...)` and ~20-row
  keyset paging (same cap as the fetch/pagination rule above — do not invent a different number).
  Machine-checked (`column-foreach-unbounded`) but deliberately narrow: only flags a `.forEach`
  chain containing a state/domain keyword (state/list/items/rows/data/records/sheds/animals/
  operators/dates/goats) inside a `verticalScroll`/`horizontalScroll` container; a fixed literal
  (`listOf(...).forEach`) or enum (`DayOfWeek.entries.forEach`) is never flagged.
- **Chips over an unbounded dimension.** Sheds/animals/operators/dates need a **searchable
  selector**, not a chip per option. Reference implementation: `FilterSelectorRow` +
  `SearchablePickerDialog` in
  `apps/goatos-android/feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeightHistoryChartScreen.kt`.
  Machine-checked in a narrow shape (`chip-row-unbounded-dimension`): a state/domain-keyword
  `.forEach { ... FilterChip/AssistChip ... }`. A chip row built without `.forEach`, or fed through
  a helper/param, is a false negative by design — still review-time via this skill.
- **Spinner over rendered content.** A refresh must never replace already-rendered rows with a
  full-screen spinner (see `docs/mobile/android-ui-quality.md` skeleton/shimmer rule below).
  Machine-checked in a narrow shape (`spinner-replaces-cached-content`): a `when {}` branch guarded
  by a bare loading flag rendering only `CircularProgressIndicator`, next to a sibling branch that
  renders non-empty cached state. A plain `if (loading) {...} else {...}` chain is a false negative
  by design; the vaccination-sheds screen additionally has a dedicated stricter check in
  `check-android-ui-foundations.mjs`.
- Bounded exception → `// compose-guard:ignore: <reason>` (same convention as the lazy-key rules
  above; one guard script covers both).

## Room SSOT / offline-first (banned: network-only screen reads)
- Every READ screen renders from **Room** (single source of truth); network refresh
  runs in background (stale-while-revalidate). Persist read → repo exposes `Flow` →
  VM observes → refresh-on-open upserts Room → re-emits. **Never a blank/loading wall
  when cache exists.**
- A thin `api.xxx()` pass-through repo with no Room persistence is **BANNED** for
  screen reads. New read models ship with Room entity + DAO + Flow from day one.
- **Pagination binds BOTH layers** — the network fetch AND the observed Room read use
  the same keyset + ~20 page. NEVER `SELECT *` / `observeAll()` / an ever-growing
  accumulated blob; the observed read is a bounded keyset window (Room `PagingSource`
  / Paging 3 + `RemoteMediator`).

## Bounded memory (machine: `make android-bounded-memory-guard`)
The retention twin of over-fetch. An in-heap cache/accumulator with no cap/TTL/
eviction, or a DAO reading a whole table into memory, OOMs low-end phones.
- Use `LruCache` or a Room `JsonBlobCacheDao` (`readCachedJson` TTL +
  `enforceCacheBounds` row/byte cap), or filter the DAO read (`WHERE status IN (...)`
  / `LIMIT` window).

## Off-main + lifecycle (memory/jank)
- Decode/parse off the Main thread: `flowOn(Dispatchers.Default)` in the repo; parse
  each field ONCE (never re-parse inside `.find`/`.filter` → O(n²)).
- Keep structured concurrency: no `GlobalScope` and no production `runBlocking`; every job has
  an explicit lifecycle owner and cancellation path.
- Screen VMs expose state via `stateIn(viewModelScope, WhileSubscribed(5_000), initial)`
  — not a forever `collectLatest` (leaks a Room collector when backgrounded).
- Compose collects `Flow` with `collectAsStateWithLifecycle`, defers fast-changing reads to the
  smallest composable, and uses `remember`/`derivedStateOf` only for measured recomposition work.
- Dynamic `LazyColumn` items need a **stable unique key** (not index) + `contentType`.
- Release camera/recorder/BT capture + observers on lifecycle stop.

## Backend contract owns visibility
The mobile UI must NOT gate visibility by role (`role ==`); render the backend-composed
nav/actions/disabled-reasons contract. Also blocks hardcoded disabled/blocked-reason
literals in production screens (preview/sample sources excluded). Machine-blocked by `make mobile-contract-ownership-guard`.

Execution affordances follow the same rule. When a backend permission means
"execute" (`task.execute`, `weighing.execute`, or a future vertical execute
permission), mobile should learn that from `/app/bootstrap.feature_flags` or a
backend action key, not from role-label code. Concretely:
- `vaccination_execute=true` means a Vaccination shed card can open Scan -> Submit.
- `weighing_execute=true` means `/weighing` renders the field execution UI and
  assignment cards can open the weighing scan/capture route.
- `false` means render display/review/monitor surfaces only.

Current director split:
- `pc_director` is Preventive Care Director: Vaccination only.
- `growth_director` is Growth Director: Weighing only.
- Operators are park-scoped and module-scoped; never infer execution from the
  `operator` role alone.

Do not add helpers like `is<Vertical>LeadershipRole(roleLabel)` to choose scan vs
display screens. If a new feature needs a component switch that the fixed Android
nav graph cannot infer from route alone, add a backend-owned feature flag derived
from the permission table and pin it with a backend bootstrap test.

Status and row action are part of that same contract. Android and admin-web must render
backend `workState`, `sopStatus`, `proofStatus`, `verificationStatus`, counts, summaries, and
`primaryActionKey`; they must not invent cross-surface vaccination states or decide locally that
an in-review row should open a different product surface.

The same ownership applies to task copy and picker data. Never display `task.title`, `scope_id`,
or UUID-bearing context as a label; render the locale-aware `TaskPresentation`. A picker with an
`option_source` must fetch the task-pinned option-values endpoint, cache that response with task
detail, submit option `value` (not label), and render backend `disabled_reason` verbatim. An empty
client fallback list or client-authored medical reason is not an acceptable substitute. Forms that
provide only inline `options` remain endpoint-independent; do not make every task of the same type
fail because an option-values endpoint was unavailable when that form never declared a source.

## Navigation stack and L0 chrome (machine: `make android-navigation-stack-guard`)
Canonical rulebook: `docs/decisions/android-navigation-stack.md`.
- **L0 only owns global chrome.** The bottom bar/drawer renders only when the
  current route exactly equals a backend-composed root route. Never infer root
  ownership with a path prefix, substring, or ancestor.
- **L1/L2/L3/L4 are hosted destinations.** Push them through the same
  `NavController`; show Up/Back, fill the `NavHost`, and hide root chrome.
- **Never reuse a root route for a drill.** Give the child a distinct route even
  when it reuses the same feature renderer. A Calendar card must not navigate to
  top-level Vaccination.
- A `ModalBottomSheet` is for temporary filters/pickers/actions, not structural
  detail. Back pops L4 → L3 → L2 → L1 → L0 and restores chrome only at L0.
- Before handoff, click the real stack on a device/emulator and capture each
  reachable level. Never describe multiple L0 states as L1/L2 evidence.

## Vaccination submit/review routing guard
The operator vaccination path is **Vaccination sheds -> Scan -> Submit ->
Vaccination sheds**. When a shed video submission syncs and the row/drive is
`submitted`, `needs_review`, or verification-pending:
- Treat backend `primaryActionKey` as the source of truth for the row's executable action.
  `scan` may open Scan; `none` must stay on the current vaccination surface unless a
  backend-owned vaccination detail/review action is added.
- Do not route to the generic `Routes.recordRoute(...)` screen as a read-only
  fallback. It is not the vaccination review/detail surface and can display
  unrelated record counts.
- If there is no dedicated vaccination detail route, keep the user on the
  Vaccination sheds screen and show the submitted/in-review status there.
- The submit ACK state must show explicit operator copy (`Submitted`/submitted
  title), not a dead disabled Submit button with only a technical sync banner.
- Keep the Vaccines-to-carry card wired to backend `carrySummary`; missing carry
  data is a backend/read-model issue, not permission to delete the card path.

## Room migrations (machine: `make room-migration-guard`)
An installed APK must survive every schema change. Room creates a DB two ways: a **fresh
install** runs `createAllTables` (every `@Entity`); an **in-place upgrade** runs ONLY the
registered `Migration`s, then validates against the `@Entity` set. So an `@Entity` added to a
`@Database` with **no migration to CREATE its table** compiles, passes fresh-install tests, and
**crashes every upgrade** on open (`Migration didn't properly handle <table>`). This shipped
(`roster_timetable_cache`/`roster_coverage_cache`, MOB-007); a plain in-memory Room test is blind
to it.
- **`exportSchema = true`** + commit `schemas/<db>/<version>.json` (the golden schema).
- **Every version bump ships its `Migration(N-1, N)`** creating exactly the new tables/columns/
  indices. **Additive + non-destructive** — no `fallbackToDestructiveMigration` (outbox holds
  unsynced writes; cache is the offline SSOT).
- **Two tests, both Robolectric/`testDebugUnitTest`:** a schema-equivalence `*MigrationTest`
  (migrate old → assert identical to fresh Room-created current) **and** an upgrade-crash
  `*UpgradeCrashTest` (seed a real old-version file via a test-only old `@Database`, reopen with
  current schema + real migrations, assert no crash + seeded rows survive).
- Rulebook: `docs/decisions/room-migration-safety.md`. Justified exception →
  `room-migration-guard:ignore: <reason>` on the `@Database` version/exportSchema line.

## Test integrity
No committed `@Ignore`/`@Disabled`/commented-`// @Test`; no flaky wall-clock
assertions; no fake-green — run `./gradlew … testStgDebugUnitTest --rerun-tasks`
and the mobile/navigation guards for REAL before claiming pass.
