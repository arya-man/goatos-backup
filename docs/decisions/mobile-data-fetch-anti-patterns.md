# Mobile (and web) list-fetch anti-patterns

A phone viewport holds ~7-10 rows at any instant. A screen that pulls 50 / 200 / 1000 rows from
Room or the backend to render is fetching far more than it can ever show — the mobile twin of the
`compute-on-read` scale anti-patterns in [scale-anti-patterns.md](./scale-anti-patterns.md). The fix
is always **fetch less**, never "parse the big list faster".

This applies to `apps/goatos-android/**` AND `apps/admin-web/**`. The web "fake/client pagination
over a capped fetch" ban already lives in `AGENTS.md`; this doc is the mobile-first statement plus
the shared rule the CI guard enforces.

## The rules

1. **Calendar week/month overview = dots only.** The grid/strip shows one marker per day = "a drive
   exists here" (optionally a severity tone). It must render from a tiny backend **day-marker** set
   (`includeDateMarkers` / `CalendarDateMarkerDto`) — **never** fetch that day's events to draw the
   grid, and never parse event dates client-side to compute the dots. A month has ~31 cells; the
   payload is ~31 markers, not hundreds of events.

2. **Every drill level paginates.** Tapping a day opens the L1 day list; L2 (sheds in a drive) and
   L3 (vaccine capture: done / pending / skipped animals) are lists too. Each is a **keyset page of
   ~20** with **infinite scroll** — prefetch the next page when the user scrolls to item ~17-18.
   **Never request more than ~20 rows in a single page**, at any level. The list must not expose a
   tappable **"Load more"** row/button for normal operator or leadership work queues. The screen owns
   continuation from viewport visibility (`LazyListState`/Paging append); the user owns the work, not
   pagination mechanics. A passive spinner/skeleton while the next page is already fetching is fine.

3. **A vaccination drive is a park visit with a mix of SHEDS, never grouped by vaccine.** One drive
   may contain one shed or many sheds, and may bundle the same or different vaccines. "Coverage by
   vaccine" is a *metric*, not the drive grouping. Do not model or fetch drives grouped by vaccine.

4. **Parse/aggregate once, off the main thread.** If a list must be transformed, parse each field
   once (never re-parse inside `.find`/`.filter` → O(n²)) and run the transform on
   `Dispatchers.Default` (ideally in the repository via `.map { }.flowOn(...)`), leaving only the
   small state assembly on Main.

## Room is the single source of truth — pagination binds BOTH layers

Offline-first makes Room the UI's source of truth: the screen renders from Room, the network refresh
upserts Room in the background. Pagination is therefore a property of BOTH sides, with the SAME keyset
and page size (~20):

- **Network fetch** requests one keyset page (`limit ~20` + `cursor`) and reads `next_cursor`.
- **The Room read the UI observes** must be an EQUALLY bounded keyset window — a Room `PagingSource`, or
  a `@Query(... ORDER BY <key> LIMIT :pageSize)` advanced by cursor. NEVER `SELECT *` / `observeAll()`,
  and never re-decode an ever-growing accumulated blob. Otherwise the over-fetch just moves from the
  network to the DB: Room re-materializes the whole cached table into memory and re-parses it on every
  emission — the same anti-pattern one layer down.

Canonical shape for a large list (scan roster, tasks): **Paging 3 + `RemoteMediator`, Room as the single
source of truth** — the mediator fills Room from the backend keyset page-by-page, a Room `PagingSource`
reads bounded windows, the VM exposes `Flow<PagingData<T>>.cachedIn(viewModelScope)`, the screen renders
`LazyColumn { items(lazyPagingItems, key = { it.id }) }`. Both layers page identically and automatically;
nothing ever holds the whole cohort.

Do not make the operator tap a visible **Load more** row/button in normal mobile work queues. Cursor
pagination is still required, but it is app-owned viewport behavior: when the lazy list reaches the
prefetch threshold (roughly item 17 in a 20-row page), the next page is fetched and upserted into Room.
The only visible pagination chrome allowed on these work lists is a passive loading footer/spinner while
the next Room-backed page is already in flight. Manual pagination buttons belong to admin/reporting
surfaces only when the product explicitly asks for page navigation.

There is no legitimate "growing blob". A JSON-blob-per-scope cache is only bounded while it holds exactly
one page; if load-more MERGES pages into that one blob it balloons — but that merge-on-append is itself
the anti-pattern, not an unavoidable nuance. The same keyset pagination applies to Room: store PER-ITEM
rows (one Room row per event/animal/task) and observe a bounded window (`ORDER BY key LIMIT :pageSize` /
`PagingSource`), so the Room read is bounded exactly like the network page and never grows. Do not cache
a whole page-response and concatenate into it.

Current gaps (tracked in [mobile-fetch-fix-backlog.md](./mobile-fetch-fix-backlog.md)): the outbox
`observeAll()` is `SELECT *` (unbounded DB read); scan/tasks need per-item Room + Paging rather than a
growing page-blob.

## CI guard

`tools/agent-hooks/check-mobile-list-fetch.mjs` (via `make mobile-guard`) blocks the machine-checkable
subset:

- `oversized-page-fetch` — a `limit = N` argument or a `*_LIMIT` / `*_PAGE_LIMIT` / `*_PAGE_SIZE`
  constant with `N > 20` in mobile code.
- `overview-parses-events` — `parseLocalDate` / `OffsetDateTime.parse` inside `buildMonthDays` /
  `buildWeekDays` (overview must consume markers, not events).
- `on2-date-scan` — re-parsing every event inside `.find` / `.any` (O(n²)).
- `unbounded-db-read` — an `@Query` that `ORDER BY`s with no `LIMIT` (e.g. `observeAll()` `SELECT *`);
  Room is the UI's source of truth, so the observed read must be a bounded ~20-row keyset window too.
- `manual-load-more-mobile-ui` — visible/manual "Load more" mobile UI in Kotlin work-list surfaces.
  Use viewport-triggered continuation instead; keep only a passive loading footer while a request is
  in flight.

**It is diff-scoped in CI**: it only scans mobile `.kt` files changed vs the base, so a commit with
no mobile code passes instantly (nothing to check). `make mobile-guard` runs the whole-tree audit
(`--all`) to show the current backlog. A genuinely-bounded case may append
`mobile-guard:ignore: <reason>` on the line (e.g. a fixed 7-cell week loop).

The static guard cannot see "does this list actually paginate on scroll" or "does the overview call
markers vs events" — those are enforced by the mobile-vaccine E2E and code review; the guard catches
the cheap, unambiguous shapes.

`make calendar-endpoint-grain-guard` is the cross-surface companion. It blocks
mobile/API/contract code that labels a narrow vaccination schedule surface but
wires it to `/calendar/vaccination/events`. Calendar event presentation may keep
that endpoint; schedule/full-schedule/mobile drive-list surfaces need a
grain-owned API/read path with its own contract and latency evidence.

## Known backlog at introduction (2026-07-12)

The guard's `--all` audit flags the current calendar + scan screens: `CalendarViewModel` /
`CalendarDayViewModel` fetch 50-200 events and the overview parses them; `ScanViewModel` fetches
**1000** rows (the L3 vaccine-capture screen renders blank because it tries to pull the whole cohort).
These are fixed in the follow-up mobile rewire, not in the guard-introduction change.

## Unbounded in-memory growth (the retention twin)

Fetch size is only half the story. Even a correctly-paged screen leaks memory if the data layer
**retains** without bound: an in-heap cache/accumulator that only ever grows, or a DAO that
observes an entire table into memory. Fast on a fresh install, an OOM after a week of use. Commit
`7058fff2` added TTL + row/byte caps + LRU eviction (`JsonBlobCacheSupport`: `readCachedJson`,
`enforceCacheBounds`, `CacheGovernance`); commit `d58acac2` bounded the outbox by observing only
ACTIVE rows and pruning SUCCEEDED instead of holding the whole table forever.

```kotlin
// BANNED — grows for the life of the process
private val cache = mutableMapOf<String, Dto>()      // no cap, no TTL, no eviction

@Query("SELECT * FROM outbox ORDER BY createdAt ASC") // whole table, incl. terminal rows
fun observeAll(): Flow<List<OutboxEntity>>
```

```kotlin
// CORRECT — bounded
private val cache = LruCache<String, Dto>(200)        // or a Room JsonBlobCacheDao with
                                                      // readCachedJson (TTL) + enforceCacheBounds
@Query("SELECT * FROM outbox WHERE status IN ('QUEUED','IN_FLIGHT')")  // active rows only
fun observeActive(): Flow<List<OutboxEntity>>
```

`make android-bounded-memory-guard`
(`tools/agent-hooks/check-android-bounded-memory.mjs`) blocks two shapes, deliberately distinct
from the fetch-SIZE rules above:

- `unbounded-inmemory-collection` — a class-field `mutableMapOf` / `mutableListOf` / `ArrayList`
  (etc.) that the file never evicts from (no `.clear` / `.remove` / `poll` / `trimToSize`, and not
  an `LruCache`). A `MutableStateFlow` of a single object is fine (constant size).
- `whole-table-read` — a DAO `@Query("SELECT * FROM t")` with **no WHERE and no LIMIT** (the
  `observeAll` shape). The `ORDER BY`-with-no-`LIMIT` variant is owned by `mobile-guard`'s
  `unbounded-db-read`; this rule catches the no-clause whole-table read it misses.

It is **diff-scoped** (a commit with no android Kotlin passes instantly), skips
`*Test`/`Fake`/`Preview`/`Entity`/`Dto` files, and honors an inline `// mobile-guard:ignore: <reason>`
for a genuinely-bounded case. `make android-bounded-memory-guard-audit` runs the whole-tree audit;
the legacy deprecated `OutboxDao.observeAll` surfaces there until it is deleted.

## Antipattern: whole-collection JSON blob for a list UI (render from a bounded SSOT)

A screen that must show a *complete* collection (e.g. the vaccination scan roster — the operator
needs every shed animal + status, and a scanned tag is validated against the full set) must render
from a **bounded per-scope Room read (the per-row SSOT)**, NOT by serializing the entire collection
into one JSON blob cache row (`json.encodeToString(wholeList)`). The blob duplicates the SSOT in the
heap and grows without bound as the scope grows.

- Tag/row validation reads the indexed SSOT DAO (`findByTag`, `countByStatus`) — never the blob.
- The UI list observes the SSOT scope (a bounded Room query), not a decoded whole-collection blob.
- The blob cache is for a bounded *window* (first page) only, paged forward, never the whole thing.

CLOSED (2026-07-20, GoatDatabase v13→v14): the scan roster no longer has a whole-collection blob.
`ScanViewModel` renders its list from `ScanRosterRowDao.observeRowsWindow` — a bounded
`ORDER BY seq LIMIT :window` keyset window over the per-row SSOT, advanced LOCALLY by scroll (the
whole roster is already in Room after `refreshScanRoster`, so page-N works offline with no extra
network). RFID validation (`findByTag`), ring/tile counters (`observeCountsByStatus`), and the submit
proof gate (`observeDoneGoatIds` + `scanRosterRowsByGoatIds`, evaluated over the FULL roster — every
DONE animal must have a synced proof video) all read the SSOT. The `ScanRosterCacheEntity` blob table,
the `refreshCompleteScanRoster` / `appendScanRoster` blob methods, and the `mergeScanRosterPage`
whole-collection helper were deleted; `MIGRATION_13_14` adds the `seq` ordering column and DROPs
`scan_roster_cache`. No whole-collection blob (or its `mobile-guard:ignore`) remains on any
scan-roster path — the only surviving ignore on `refreshScanRoster` is the function-local
`seenCursors` set that guarantees forward progress (bounded by one shed's page count, GC'd on
return), which is a memory-guard false positive, not a fetch or a serialized roster.

## Verified NON-issues (do not re-flag as bugs)

Re-verified against `origin/main` 05889b83 on 2026-07-20:

- **Proof read cap (`ProofCaptureDao.observeForTask`/`listForTask` `LIMIT MAX_PROOFS_PER_TASK=10_000`)**
  is an UNREACHABLE safety bound, not a silent truncation: a task's proofs are bounded by
  `MAX_PROOFS_PER_GOAT=5` × the shed's animals (hundreds at most) — orders of magnitude below 10k.
  It is not a live data-loss bug.
- **Crash-recovery re-enqueue must preserve proof capture source.** `capture_source` is now
  SOP-controlled: per-goat proof remains `in_app_camera`, while shed-level proof may use
  `gallery_picker`. Any recovery/outbox path must persist and replay the capture source from Room;
  it must not silently rebuild upload metadata from `ProofPolicy.Default`.
- **`minimumCountPerSubject`** is enforced server-side at SOP submission
  (`backend/internal/sop/app/service.go` `validatePerGoatProofRefs`), the correct boundary. The Android
  client parses it for display only; that is not a missing-enforcement bug.

## Antipattern: routing vaccination review back into the generic Record screen

The vaccination operator flow is **Vaccination sheds -> Scan -> Submit -> Vaccination sheds**.
After a shed video submission is synced, the operator should see the shed/drive as submitted or
in review on the Vaccination sheds list. Do **not** route that state into the old generic
`/record` surface just because scanning is no longer allowed.

Backend owns this state and the executable row action for both mobile and admin-web. Clients render
the backend `workState`, `sopStatus`, `proofStatus`, `verificationStatus`, counts, `carrySummary`,
and `primaryActionKey`. They must not author a separate "in review", "done", or "open record" truth
from local status combinations.

The generic Record screen is not the vaccination shed-submit review surface: it can show unrelated
record counts such as `0 doses` / `0 / 1 done`, which is worse than doing nothing because it
contradicts the backend submit state. For vaccination execution rows:

- `primaryActionKey=scan` may open the Scan flow. `primaryActionKey=none` must stay on the
  vaccination surface unless a backend-owned vaccination detail/review action is added.
- `submitted`, `needs_review`, and verification-pending states must stay in the vaccination
  execution surface unless there is a dedicated vaccination review/detail route.
- A click on an in-review/completed vaccination shed must never navigate to `Routes.recordRoute(...)`
  as a fallback. If there is no correct detail surface, keep the user on Vaccination sheds.
- The submit screen's synced/acked state must use explicit operator copy such as `Submitted`, not
  a dead disabled `Submit` button with only an outbox technical banner.
- The Vaccines-to-carry card is part of the Vaccination sheds screen. It renders only from
  backend `carrySummary`; do not delete or hide the card path to work around missing backend data.

Regression proof for this class is a phone/emulator UI check of the real stack, not source
inspection: submit a shed, confirm the app returns to Vaccination sheds, confirm the submitted
state is visible there, and tap the row to prove it does not open the generic Record screen.

## Compose lazy-list key correctness (machine: `make android-compose-lists-guard`)

`LazyColumn`/`LazyRow`/`LazyVerticalGrid` item identity is the key. Two rules,
both enforced by `tools/agent-hooks/check-android-compose-lists.mjs` (diff-scoped
against `origin/main`; a genuinely-bounded case appends
`compose-guard:ignore: <reason>` on the line):

- **`lazy-list-entity-id-key` (crash).** Never key a per-ROW list by a per-ENTITY
  id. The scan roster renders one row per **obligation**, so keying by `goatId`
  put a goat with two due vaccines (ET+TT · PPR) on two rows with the SAME key →
  `java.lang.IllegalArgumentException: Key "<uuid>" was already used. If you are
  using LazyColumn/Row please make sure you provide a unique key for each item.`
  thrown in the LazyList **measure pass**, which pops the whole screen. This
  shipped in `0.1.6-stg` (Crashlytics, field operators) and was fixed in
  `a9c35a1d` by keying on the unique per-row `obligationId`. The proof-needed
  feed hit the same class again when staging carried multiple active vaccine
  obligations per goat; the row key must prefer `obligationId` before `goatId`.
  The guard flags a `key = { it.goatId }`-style bare entity selector (`goatId`,
  `animalId`, `goatUuid`, …) and Elvis/fallback keys where the entity id wins
  before the row id. A string-template/composite key is allowed only when no
  row id exists and the composite includes enough row-grain fields, e.g.
  `goatId|vaccineLabel|primaryTag`.
- **`lazy-list-missing-key` (state loss).** `items(<collection>)` /
  `itemsIndexed(<collection>)` with no `key =` falls back to positional identity,
  so an insert/remove/reorder reuses an item's remembered state (checkbox,
  expand, scroll) for the WRONG row and some mutations crash. Supply
  `key = { it.<uniqueRowId> }`. The count overload `items(<Int>)` is exempt (it
  has no key parameter).

Rule of thumb: **the key is the unique identity of the RENDERED ROW, not of the
domain object it happens to show.** When a list can hold more than one row per
entity, the entity id is not a valid key.

## Phone-scale UI: chips, render-everything, and spinner-over-cache (machine: `make android-compose-lists-guard`)

Three more shapes of the same "fetch/render less than the whole cohort" discipline, all enforced
by the same guard (`tools/agent-hooks/check-android-compose-lists.mjs`) that owns the lazy-list key
rules above. A real park is ~100 sheds x ~70-90 animals — a task export is ~8,000 rows; a phone
list shows ~10, never more than ~20, and every drill level paginates ~20 (rule 2 above). Full
rulebook + before/after examples: [`apps/goatos-android/docs/phone-scale-ui.md`](../../apps/goatos-android/docs/phone-scale-ui.md).

- **`chip-row-unbounded-dimension`.** Chips are one pill per element — correct only for a small
  FIXED set (2-3 values, e.g. an individual/lump-sum toggle). Never chip sheds, animals, operators,
  dates, or parks; use the `FilterSelectorRow` + `SearchablePickerDialog` searchable-selector
  pattern in `WeightHistoryChartScreen.kt` instead.
- **`column-foreach-unbounded` / `nested-scroll-in-lazy-items`.** A `<state>.forEach { }` inside a
  scrollable `Column`/`Row`, or a `Lazy*`/scrollable container nested inside another list's
  `items()` row, inflates and measures every row up front instead of windowing. Use
  `LazyColumn`/`LazyRow` with `items(list, key = ...)` and keep lists out of other lists' rows.
- **`spinner-replaces-cached-content`.** A full-screen `CircularProgressIndicator` guarded by a
  bare loading flag (not compounded with a cache-emptiness check) next to a sibling branch that
  renders cached content tears that content down on every refresh. Compound the condition
  (`state.loading && state.items.isEmpty() -> ...`, the pattern in
  `WeighingLeadershipVideosScreen.kt`) or annotate/overlay instead.

All three are deliberately conservative (narrow allowlists, by-design false negatives over false
positives) — see the header comment in `check-android-compose-lists.mjs` for the exact scope. A
genuinely-bounded case may append `compose-guard:ignore: <reason>` on the line.

## Android row-action scope (machine: `make android-row-action-scope-guard`)

Repeated mobile cards must not share a screen-wide in-flight gate for row-level
actions. If the UI renders one card per animal, shed, obligation, proof, or
assignment, that row's Save/Update/Retry action must be blocked only by state
scoped to that row identity. A global `actionInFlight`, `busy`, or screen submit
flag is reserved for screen-wide operations such as final submit, navigation,
or a modal transaction that truly locks the whole surface.

Field failure class: individual weighing free-flow saved the previous animal
card successfully, then the next card randomly could not save because animal A's
pending save/update held the global `actionInFlight` gate. That is invalid for
free-flow row saves. The row action must track `updatingAnimalIds` or equivalent
row-keyed state, and row `canSave*` must not read the global busy flag.

Required proof for this class:

- A regression test starts saving animal A and keeps that save pending.
- The UI/view-model state still allows animal B's Save while animal A is pending.
- Submitting animal B records a second capture without waiting for animal A.

The static guard `tools/agent-hooks/check-android-row-action-scope.mjs` enforces
the current weighing path: `recordIndividual(animalId, rawWeight)` must call
`recordIndividualRow(..., useGlobalBusyGate = false)`, per-row duplicate saves
must use `updatingWeightAnimalIds`, and row `canSaveWeight` must not depend on
global `busy`/`actionInFlight`.
