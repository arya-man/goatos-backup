# Mobile/web list-fetch remediation backlog

Companion to [mobile-data-fetch-anti-patterns.md](./mobile-data-fetch-anti-patterns.md) (the rule)
and its CI guard (`make mobile-guard`). This is the tracked, prioritized fix list from an exhaustive
whole-app audit (6 parallel area auditors + a best-practices verification pass), 2026-07-12.

> The audit's best-practices review ran WITHOUT a live context7 docs pull (its API key was rejected
> that session); the canonical patterns below reflect current Android/Compose/coroutines/Paging
> guidance from model knowledge and should be re-confirmed against context7 before the final sweep.

## Canonical patterns (apply app-wide)

1. **Repo Flow parses off-Main, once.** Every `observeX()` in core-data:
   `dao.observe(key).map { it.toResource() /* decode + DTO->Ui */ }.flowOn(Dispatchers.Default)`.
2. **ViewModel state = `stateIn(WhileSubscribed(5_000))`, never a forever-collector.** Do not bridge a
   cold Room Flow into a `MutableStateFlow` via `viewModelScope.launch { collectLatest {} }` (runs
   forever, even backgrounded). `SyncStatusViewModel`/`SessionViewModel` are the in-repo reference.
3. **Never over-fetch a screen.** One keyset page (~20), never a whole cohort. `SCAN_PAGE_SIZE`,
   `DAY_PAGE_LIMIT`, `CALENDAR_PAGE_LIMIT`, `GAPS_LIMIT`, `REVIEW_LIMIT`, `COVERAGE_LIMIT` = 20.
4. **Keyset/cursor over OFFSET** (matches backend `next_cursor` + the scale ADR). Request page_size,
   read `next_cursor`, append into the same Room scope, prefetch when last-visible index >= size-3.
   Paging 3 (`RemoteMediator` + Room) only for the largest unbounded lists (scan roster, tasks).
5. **Lazy lists always key items; never `forEach` a data list.** Bottom-sheet lists =
   `LazyColumn(Modifier.heightIn(max=…))`, never `Column { forEach }` (eager compose + clip).
6. **Calendar overview = day-markers only.** Grid dots from a backend marker set, never fetch/parse
   a day's events to draw the grid. Only drill lists fetch events, and they paginate.
7. **Offline-first: no network-only screen reads.** Every screen-facing repo needs Room + DAO + Flow.
8. **Bounded, pruned device tables.** Outbox: summary via `COUNT(*) GROUP BY status`; list = ACTIVE
   rows + a bounded recent-terminal window; prune SUCCEEDED in the drain pass (never touch
   QUEUED/IN_FLIGHT/FAILED). Derive app-wide status lazily, not on an eager never-cancelled scope.
9. **No Context/View/Activity in ViewModels;** vendor ports `@Singleton @ApplicationContext` (RFID
   reader already correct). Collect hot device flows in `viewModelScope`, disable in `onCleared()`.
10. **O(1) matching, not O(n)-per-event on Main.** Precompute a `Map<tag, indices>` when the roster
    loads (off-Main); `_state.update` does only the targeted copy.

## Backend contract dependencies

- **scan-roster**: client must send required `task_id` + a `cursor`; add `next_cursor` +
  `taskId`/`batchId`/`sopVersionId`/`taskRowVersion` to `ScanRosterResponseDto`. Backend already
  hard-requires `task_id` (400 `invalid_task_id`) and returns keyset `NextCursor` — wiring, not new logic.
- **GET /vaccination/execution**: add `next_cursor` + `cursor` (L1 rows, L2 sheds).
- **GET /app/tasks**: add `next_cursor` + `cursor`.
- **GET /app/roster/timetable**: add `next_cursor` + `cursor` (or certify centers <=20 seats).
- **Day-marker read model**: per-day markers independent of the event list (calendar dots).
- **vaccination gaps**: confirm `next_cursor` on the response (cursor input already accepted).
- **verification queue**: NO change (already returns `next_cursor` + accepts `cursor`) — wiring only.
- **coverage**: deferrable (bounded per-vaccine axis); just lower client limit to 20.
- **admin-web**: control-tower is cursor-only (stop sending `offset`); shed-drilldown needs
  `limit`+`cursor`+`next_cursor`; shed-board must migrate OFFSET → keyset SQL.

## Prioritized units

### P0 — operator L3 vaccine capture (scan roster) — BLANK screen + whole-cohort fetch
- **A.** `getScanRoster` sends no `task_id` → backend 400s every call → Room never caches → blank.
  Add `task_id`+`cursor` to Retrofit `getScanRoster`/`AppApi` + `ScanRosterResponseDto.nextCursor`;
  thread `taskId`+`cursor` through `ExecutionRepository.{observe,refresh,}ScanRoster`; `ScanViewModel`
  `limit=1000` → `SCAN_PAGE_SIZE=20` keyset. Files: ScanViewModel, NetworkModule, AppApi, ScanDto,
  ExecutionRepository.
- **B.** `ScanScreen` roster `LazyColumn` → infinite scroll (`ScanEvent.LoadMore` → `appendScanRoster`).
- **NOTE:** an active "vaccination-closure" session is CURRENTLY rewriting this exact path (untracked
  `scan_roster_cursor.go`/`execution_cursor.go` + dirty ScanViewModel/ExecutionRepository/ScanDto/
  ScanScreen). Do NOT double-rewrite — reconcile with their landed cursor contract, then apply the
  ~20 page size + `stateIn`/`flowOn` items if they haven't.

### P1
- **L2 sheds** (`ShedsViewModel`/`ExecutionRepository`/`ShedsScreen`): unbounded fetch (no limit) →
  ~20 keyset; `applyResource` → `withContext(Default)`; add `ShedsEvent.LoadMore` + list pagination.
- **Calendar** (`CalendarViewModel`/`CalendarDayViewModel`): `CALENDAR_PAGE_LIMIT`/`DAY_PAGE_LIMIT`
  50 → 20.
- **Tasks** (`TasksRepository`): network-only → add Room cache + `observeTasks`/`refreshTasks`; add
  `next_cursor` + paging.
- **Repo off-Main decode** (dedups 3× ControlTower + per-repo): `.flowOn(Default)` on
  ControlTowerRepository.observeSummary, VaccinationInsightsRepository.observe{Gaps,Coverage},
  ExecutionRepository.observe{Rows,Shed,ScanRoster}, CalendarRepository.observeEvents,
  AdherenceRepository.observeAdherence; move VM DTO→Ui maps into the flow.
- **Review queue** (`LeadershipViewModel`): `REVIEW_LIMIT` 50→20, retain `nextCursor`, append on
  scroll (contract already supports it — wiring only).
- **Data-gaps overlay** (`Overlays.kt`/`LeadershipViewModel`): `forEach` Column →
  `LazyColumn(heightIn)`; `GAPS_LIMIT` 50→20 + cursor paging.
- **Review-queue offline cache** (`VaccinationReviewRepository`): network-only → Room + cursor.
- **Timetable/coverage offline cache** (`RosterRepository`): network-only → Room; bound fetch to 20.
- **Outbox** (`OutboxDao`/`SyncRepository`/`OutboxStore`): unbounded `observeAll()` + never-pruned
  SUCCEEDED on process-lifetime scope → `COUNT(*) GROUP BY status` summary + ACTIVE/bounded list +
  SUCCEEDED retention prune.
- **Cross-cutting: `stateIn(WhileSubscribed)`** across all read VMs (Sheds, Scan, Calendar,
  CalendarDay, Leadership, Alerts, Timetable) — replace forever-collectors.

### P2
- Sync sheet render: map `SyncQueueItem`→`SyncItem` once off-Main, not per recomposition.
- `COVERAGE_LIMIT` 50→20.
- admin-web: control-tower (offset→ct_cursor keyset; stale `pageResult`/`maxPageFor` imports may be
  broken), shed-drilldown (add limit+cursor+next_cursor), shed-board (OFFSET→keyset SQL).

## Sequencing note

Almost the entire mobile tree is under concurrent multi-session churn (scan/execution closure work in
flight). Fixes should reconcile against the settled base, not race it. Recommended order once the
scan/execution rewrite lands: P0 reconciliation → repo `.flowOn` sweep (low-risk, mechanical) →
`stateIn(WhileSubscribed)` sweep → offline-cache repos (Tasks/Roster/Review) → outbox bounding →
web. Each unit validated on-device + isolated push; a final Opus + context7 best-practices review
over the whole diff before the last push.
