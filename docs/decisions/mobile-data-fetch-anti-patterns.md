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
   **Never request more than ~20 rows in a single page**, at any level.

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

**It is diff-scoped in CI**: it only scans mobile `.kt` files changed vs the base, so a commit with
no mobile code passes instantly (nothing to check). `make mobile-guard` runs the whole-tree audit
(`--all`) to show the current backlog. A genuinely-bounded case may append
`mobile-guard:ignore: <reason>` on the line (e.g. a fixed 7-cell week loop).

The static guard cannot see "does this list actually paginate on scroll" or "does the overview call
markers vs events" — those are enforced by the mobile-vaccine E2E and code review; the guard catches
the cheap, unambiguous shapes.

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
- **Crash-recovery re-enqueue uses `ProofPolicy.Default`** (`reconcileRecoverableUploadsNow` →
  `enqueueRegistrationNow` with no policy). The only field this affects is `capture_source`, which is
  camera-only-enforced and effectively constant (`in_app_camera`), so the "loss" is a no-op. If a
  non-default `capture_source` is ever introduced, persist the policy on the proof row (Room bump +
  MigrationTest + UpgradeCrashTest) and use it in recovery — until then this is not a live bug.
- **`minimumCountPerSubject`** is enforced server-side at SOP submission
  (`backend/internal/sop/app/service.go` `validatePerGoatProofRefs`), the correct boundary. The Android
  client parses it for display only; that is not a missing-enforcement bug.
