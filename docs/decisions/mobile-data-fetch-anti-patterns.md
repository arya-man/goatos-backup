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

3. **A vaccination drive is a mix of SHEDS, never grouped by vaccine.** A shed may bundle the same
   or different vaccines (micro-drives, combinations), but the drive is shed-scoped. "Coverage by
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
