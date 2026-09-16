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
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.PenRoutineDetailCacheEntity
import sg.mesha.goatos.core.data.cache.PenRoutineItemEntity
import sg.mesha.goatos.core.data.cache.PenRoutineRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.PenRoutineDetailDto
import sg.mesha.goatos.core.network.dto.PenRoutineTaskDto
import sg.mesha.goatos.core.network.dto.PenRoutineFilterDto

/** One screen-page of routine tasks — bounds BOTH the network request and the Room window
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val PEN_ROUTINE_PAGE_SIZE = 20

/** How many distinct filter scopes keep their cached list rows. */
private const val PEN_ROUTINE_CACHED_QUERIES = 4

/** Bump whenever the cached row JSON changes shape incompatibly. */
private const val PEN_ROUTINE_CACHE_SHAPE = "routine-v1"

/**
 * The whole-page facts the backend composes beside the rows on every list refresh: its title,
 * the filter chips (labels, counts, empty copy) and the caller's open count. Rendered verbatim;
 * the client derives none of it.
 */
data class PenRoutinePageMeta(
    val title: String = "",
    val filters: List<PenRoutineFilterDto> = emptyList(),
    val openCount: Int = 0,
)

/**
 * Pen routines (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md). READS are
 * offline-first per docs/decisions/android-offline-first.md: the list and the detail both render
 * from Room and network refreshes upsert Room. The WRITES — the presence punch and the submit —
 * do not live here: they ride the durable outbox through `SyncRepository.enqueuePenRoutine*`,
 * and the sync engine reconciles each successful write's returned task back through
 * [persistServerDetail].
 */
interface PenRoutinesRepository {
    /** The paged task list for one backend filter KEY ("" = the backend default). */
    fun tasks(filter: String): Flow<PagingData<PenRoutineTaskDto>>

    /** Backend-composed page facts from the LAST list refresh. */
    val pageMeta: StateFlow<PenRoutinePageMeta>

    /** Drops one scope's freshness marker so the next pager refetches instead of TTL-skipping. */
    suspend fun invalidateTasks(filter: String)

    /** Room-first task detail; null while nothing is cached yet. */
    fun observeTask(taskId: String): Flow<PenRoutineTaskDto?>

    /** Network -> Room detail refresh. Non-blocking contract: a failure leaves the cache serving. */
    suspend fun refreshTask(taskId: String)

    /**
     * Reconciles a successful submit's RETURNED task into Room — the detail cache AND every
     * cached list-row copy — and drops every list scope's freshness marker (a done task moves
     * from the To do chip to the Done chip). Called by the sync engine only.
     */
    suspend fun persistServerDetail(detail: PenRoutineDetailDto)
}

class DefaultPenRoutinesRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : PenRoutinesRepository {

    private val _pageMeta = MutableStateFlow(PenRoutinePageMeta())
    override val pageMeta: StateFlow<PenRoutinePageMeta> = _pageMeta

    @OptIn(ExperimentalPagingApi::class)
    override fun tasks(filter: String): Flow<PagingData<PenRoutineTaskDto>> {
        val key = scopeKey(filter)
        return Pager(
            config = PagingConfig(
                pageSize = PEN_ROUTINE_PAGE_SIZE,
                initialLoadSize = PEN_ROUTINE_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = PEN_ROUTINE_PAGE_SIZE * 3,
            ),
            remoteMediator = PenRoutineRemoteMediator(
                filter = filter,
                api = api,
                database = database,
                json = json,
                clock = clock,
                onMeta = { meta -> _pageMeta.value = meta },
            ),
            pagingSourceFactory = { database.penRoutineItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<PenRoutineTaskDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateTasks(filter: String) {
        database.penRoutineRemoteKeyDao().delete(scopeKey(filter))
    }

    override fun observeTask(taskId: String): Flow<PenRoutineTaskDto?> =
        database.penRoutineDetailCacheDao().observe(taskId)
            .map { entity ->
                readCachedJson<PenRoutineTaskDto>(
                    json = json,
                    cacheKey = taskId,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.penRoutineDetailCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshTask(taskId: String) {
        // exception:exempt expected refresh failure (offline/timeout/5xx); the cache keeps serving
        // and the next successful open/refresh repairs it — the non-blocking refresh contract.
        runCatching { persistServerDetail(api.getPenRoutine(taskId), invalidateLists = false) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "pen_routine_detail_refresh_failed task=$taskId", it)
            }
    }

    override suspend fun persistServerDetail(detail: PenRoutineDetailDto) {
        persistServerDetail(detail, invalidateLists = true)
    }

    private suspend fun persistServerDetail(detail: PenRoutineDetailDto, invalidateLists: Boolean) {
        val task = detail.task
        if (task.taskId.isBlank()) return
        val now = clock()
        val detailDao = database.penRoutineDetailCacheDao()
        val itemDao = database.penRoutineItemDao()
        val taskJson = json.encodeToString(task)
        database.withTransaction {
            // MONOTONIC on the server's own row_version: a payload OLDER than what Room already
            // holds is never written back. The sync engine replays a succeeded write's stored
            // response after a restart, and that response is the task as it was when the write
            // landed; if the screen has since refreshed a newer row, the replay must lose.
			val cached = detailDao.get(task.taskId)?.dtoJson
				?.let {
					runCatching { json.decodeFromString<PenRoutineTaskDto>(it) }
						.onFailure { error -> android.util.Log.w(LOG_TAG, "pen_routine_detail_cached_decode_failed task=${task.taskId}", error) }
						.getOrNull()
				}
            if (cached != null && cached.rowVersion > task.rowVersion) {
                android.util.Log.w(
                    LOG_TAG,
                    "pen_routine_detail_stale_ignored task=${task.taskId} cached_rv=${cached.rowVersion} incoming_rv=${task.rowVersion}",
                )
                return@withTransaction
            }
            detailDao.upsert(PenRoutineDetailCacheEntity(cacheKey = task.taskId, dtoJson = taskJson, updatedAt = now))
            // Every cached list-row copy adopts the server's fresh task so a re-entered list shows
            // the new chip without a refetch.
            itemDao.upsertAll(itemDao.rowsForTask(task.taskId).map { row -> row.copy(dtoJson = taskJson, updatedAt = now) })
            if (invalidateLists) {
                // A write moves the task between chips; dropping the markers makes the next
                // pager refresh refetch every scope while the cached rows keep serving.
                database.penRoutineRemoteKeyDao().deleteAll()
            }
        }
        detailDao.enforceCacheBounds()
    }

    private fun scopeKey(filter: String): String =
        cacheKey(PEN_ROUTINE_CACHE_SHAPE, "pen-routines", filter, PEN_ROUTINE_PAGE_SIZE.toString())

    private companion object {
        const val LOG_TAG = "GoatOsPenRoutines"
    }
}

/**
 * Fills Room from `GET /app/pen-routines` page-by-page over the endpoint's opaque keyset cursor.
 * Room stays the single source of truth: this never hands rows to the UI, it only writes them,
 * and the [androidx.paging.PagingSource] re-emits. ALWAYS refresh on open: the kernel raises a
 * task each business day, so a list cached last night is stale by definition.
 */
@OptIn(ExperimentalPagingApi::class)
private class PenRoutineRemoteMediator(
    private val filter: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val onMeta: (PenRoutinePageMeta) -> Unit,
) : RemoteMediator<Int, PenRoutineItemEntity>() {
    private val queryKey = cacheKey(PEN_ROUTINE_CACHE_SHAPE, "pen-routines", filter, PEN_ROUTINE_PAGE_SIZE.toString())

    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, PenRoutineItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.penRoutineRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = api.getPenRoutines(
                filter = filter.ifBlank { null },
                limit = PEN_ROUTINE_PAGE_SIZE,
                cursor = cursor,
            )
            if (loadType == LoadType.REFRESH) {
                // Page facts ride the refresh only: counts are whole-list, so an APPEND page
                // fetched long after the user last looked must not overwrite them.
                onMeta(PenRoutinePageMeta(title = response.title, filters = response.filters, openCount = response.openCount))
            }
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            // A cursor that did not ADVANCE is also the end, or an echoing backend would spin
            // this mediator forever on one page.
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            database.withTransaction {
                val itemDao = database.penRoutineItemDao()
                val remoteKeyDao = database.penRoutineRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.rows.mapIndexed { index, task ->
                        PenRoutineItemEntity(
                            queryKey = queryKey,
                            grainKey = task.taskId,
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(task),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    PenRoutineRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor.orEmpty(),
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteRowsOutsideNewestQueries(PEN_ROUTINE_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(PEN_ROUTINE_CACHED_QUERIES)
                }
            }
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            MediatorResult.Error(error)
        }
    }
}
