package sg.mesha.goatos.core.data

import androidx.paging.ExperimentalPagingApi
import androidx.paging.LoadType
import androidx.paging.Pager
import androidx.paging.PagingConfig
import androidx.paging.PagingData
import androidx.paging.PagingState
import androidx.paging.RemoteMediator
import androidx.paging.map
import androidx.room.withTransaction
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.ToxinTaskDetailCacheEntity
import sg.mesha.goatos.core.data.cache.ToxinTaskItemEntity
import sg.mesha.goatos.core.data.cache.ToxinTaskRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ToxinTaskDetailDto
import sg.mesha.goatos.core.network.dto.ToxinTaskDto
import sg.mesha.goatos.core.network.dto.ToxinTaskFilterDto

/** One screen-page of toxin test tasks — bounds BOTH the network request and the Room window
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val TOXIN_PAGE_SIZE = 20

/** How many distinct status-filter scopes keep their cached list rows. */
private const val TOXIN_CACHED_QUERIES = 6

/** Bump whenever the cached task-row JSON changes shape incompatibly (see PACKING_CACHE_SHAPE's
 *  kdoc in FeedRepository.kt for why a stale-shape row must be orphaned, never leniently decoded). */
private const val TOXIN_CACHE_SHAPE = "task-v1"

/**
 * Toxin module reads (aflatoxin strip test, maintainer decision 2026-08-25). Offline-first per
 * docs/decisions/android-offline-first.md: Room is the UI's single source of truth — the task
 * list and the task detail both render from Room and network refreshes upsert Room. WRITES do
 * not live here: step completions and the reading submit ride the durable outbox through
 * `SyncRepository.enqueueToxinStepComplete` / `enqueueToxinSubmit`, and the sync engine
 * reconciles each successful write's returned detail back through [persistServerDetail].
 *
 * Step STATES stay SERVER-owned: a cached detail renders instantly, but the countdown/gate truth
 * it carries is whatever the server last composed — the screen refreshes on open/resume and
 * never derives its own gate from the device clock.
 */
interface ToxinRepository {
    /** The paged task list for one backend filter KEY ("" = the backend default, All), a Room
     *  PagingSource filled by a RemoteMediator. */
    fun tasks(filter: String): Flow<PagingData<ToxinTaskDto>>

    /** The backend-composed filter chips from the LAST list refresh, in display order. */
    val filters: StateFlow<List<ToxinTaskFilterDto>>

    /** Whole-tenant status counts from the LAST list refresh (never page-local sums). */
    val statusCounts: StateFlow<Map<String, Int>>

    /** Drops one scope's freshness marker so the next pager refetches instead of TTL-skipping.
     *  Cached rows keep serving until fresh rows land. */
    suspend fun invalidateTasks(filter: String)

    /** Room-first task detail; null while nothing is cached yet (corrupt rows quarantine). */
    fun observeTaskDetail(taskId: String): Flow<ToxinTaskDetailDto?>

    /** Network -> Room detail refresh. Non-blocking contract: a failure leaves the cache serving. */
    suspend fun refreshTaskDetail(taskId: String)

    /**
     * Reconciles a successful step-complete/submit dispatch's RETURNED detail into Room — the
     * detail cache AND every cached list-row copy of the task. Called by the sync engine only.
     */
    suspend fun persistServerDetail(detail: ToxinTaskDetailDto)
}

class DefaultToxinRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : ToxinRepository {

    private val _statusCounts = MutableStateFlow<Map<String, Int>>(emptyMap())
    override val statusCounts: StateFlow<Map<String, Int>> = _statusCounts

    private val _filters = MutableStateFlow<List<ToxinTaskFilterDto>>(emptyList())
    override val filters: StateFlow<List<ToxinTaskFilterDto>> = _filters

    @OptIn(ExperimentalPagingApi::class)
    override fun tasks(filter: String): Flow<PagingData<ToxinTaskDto>> {
        val key = scopeKey(filter)
        return Pager(
            config = PagingConfig(
                pageSize = TOXIN_PAGE_SIZE,
                initialLoadSize = TOXIN_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = TOXIN_PAGE_SIZE * 3,
            ),
            remoteMediator = ToxinTaskRemoteMediator(
                filter = filter,
                api = api,
                database = database,
                json = json,
                clock = clock,
                onCounts = { counts -> _statusCounts.value = counts },
                onFilters = { chips -> _filters.value = chips },
            ),
            pagingSourceFactory = { database.toxinTaskItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<ToxinTaskDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateTasks(filter: String) {
        // Deleting the remote key makes the next mediator initialize() LAUNCH_INITIAL_REFRESH;
        // the item rows are left in place so the screen keeps rendering until fresh rows land.
        database.toxinTaskRemoteKeyDao().delete(scopeKey(filter))
    }

    override fun observeTaskDetail(taskId: String): Flow<ToxinTaskDetailDto?> =
        database.toxinTaskDetailCacheDao().observe(taskId)
            .map { entity ->
                readCachedJson<ToxinTaskDetailDto>(
                    json = json,
                    cacheKey = taskId,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.toxinTaskDetailCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshTaskDetail(taskId: String) {
        // exception:exempt expected refresh failure (offline/timeout/5xx); the cache keeps serving
        // and the next successful open/refresh repairs it — the non-blocking refresh contract.
        runCatching { persistServerDetail(api.getToxinTask(taskId)) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "toxin_detail_refresh_failed task=$taskId", it)
            }
    }

    override suspend fun persistServerDetail(detail: ToxinTaskDetailDto) {
        if (detail.taskId.isBlank()) return
        val now = clock()
        val detailDao = database.toxinTaskDetailCacheDao()
        val itemDao = database.toxinTaskItemDao()
        val taskJson = json.encodeToString(detail.toTask())
        database.withTransaction {
            detailDao.upsert(
                ToxinTaskDetailCacheEntity(
                    cacheKey = detail.taskId,
                    dtoJson = json.encodeToString(detail),
                    updatedAt = now,
                ),
            )
            // Every cached list-row copy of this task, across status scopes, adopts the server's
            // fresh task half so a re-entered list shows the new chip/progress without a refetch.
            itemDao.upsertAll(
                itemDao.rowsForTask(detail.taskId).map { row ->
                    row.copy(dtoJson = taskJson, updatedAt = now)
                },
            )
        }
        detailDao.enforceCacheBounds()
    }

    private fun scopeKey(filter: String): String =
        cacheKey(TOXIN_CACHE_SHAPE, "toxin-tasks", filter, TOXIN_PAGE_SIZE.toString())

    private companion object {
        const val LOG_TAG = "GoatOsToxin"
    }
}

/**
 * Fills Room from `GET /app/toxin/tasks` page-by-page over the endpoint's opaque keyset cursor.
 * Room stays the single source of truth: this never hands rows to the UI, it only writes them,
 * and the [androidx.paging.PagingSource] re-emits.
 *
 * ALWAYS refresh on open: this is a work queue answering "which tests are waiting on someone
 * RIGHT NOW", and steps are person-independent — another authorized person may have advanced a
 * task since the last cache write. Paging renders the cached Room window immediately while the
 * refresh runs, and a failed refresh leaves those cached rows on screen
 * (docs/decisions/android-offline-first.md — refresh-on-open, stale-while-revalidate).
 */
@OptIn(ExperimentalPagingApi::class)
private class ToxinTaskRemoteMediator(
    private val filter: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val onCounts: (Map<String, Int>) -> Unit,
    private val onFilters: (List<ToxinTaskFilterDto>) -> Unit,
) : RemoteMediator<Int, ToxinTaskItemEntity>() {
    private val queryKey = cacheKey("task-v1", "toxin-tasks", filter, TOXIN_PAGE_SIZE.toString())

    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, ToxinTaskItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.toxinTaskRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = api.getToxinTasks(
                filter = filter.ifBlank { null },
                limit = TOXIN_PAGE_SIZE,
                cursor = cursor,
            )
            if (loadType == LoadType.REFRESH) {
                onCounts(response.statusCounts)
                // Chips ride the same refresh: their counts are whole-tenant, so they must not be
                // updated from an APPEND page, which carries the same aggregates but is fetched
                // long after the user last saw the list.
                if (response.filters.isNotEmpty()) onFilters(response.filters)
            }
            // An absent next_cursor is the contract's own end-of-pages signal; a cursor that did
            // not ADVANCE is also the end, or an echoing backend would spin this mediator forever
            // on one page (the non-terminating pagination loop the scale rules ban).
            val nextCursor = response.nextCursor.takeIf { it.isNotBlank() }
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            // Page rows and their cursor commit TOGETHER, or a crash between them leaves the
            // cursor pointing past rows that were never stored.
            database.withTransaction {
                val itemDao = database.toxinTaskItemDao()
                val remoteKeyDao = database.toxinTaskRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    // A refresh re-reads the list from the top: drop the scope's rows so a task
                    // cancelled/finished elsewhere disappears instead of lingering as a ghost.
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.tasks.mapIndexed { index, task ->
                        ToxinTaskItemEntity(
                            queryKey = queryKey,
                            grainKey = task.taskId,
                            // Server order preserved by offsetting the page's own index, so a
                            // later page never sorts above an earlier one.
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(task),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    ToxinTaskRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor.orEmpty(),
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteRowsOutsideNewestQueries(TOXIN_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(TOXIN_CACHED_QUERIES)
                }
            }
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            // Room keeps whatever it already had: a failed page load surfaces as a Paging
            // LoadState.Error the screen renders next to the cached rows, never as a wipe.
            MediatorResult.Error(error)
        }
    }
}
