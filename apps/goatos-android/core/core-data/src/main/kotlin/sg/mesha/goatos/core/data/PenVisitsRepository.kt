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
import sg.mesha.goatos.core.data.cache.PenVisitDetailCacheEntity
import sg.mesha.goatos.core.data.cache.PenVisitItemEntity
import sg.mesha.goatos.core.data.cache.PenVisitRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.PenVisitDetailDto
import sg.mesha.goatos.core.network.dto.PenVisitDto
import sg.mesha.goatos.core.network.dto.PenVisitFilterDto

/** One screen-page of pen visits — bounds BOTH the network request and the Room window
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val PEN_VISIT_PAGE_SIZE = 20

/** How many distinct filter scopes keep their cached list rows. */
private const val PEN_VISIT_CACHED_QUERIES = 4

/** Bump whenever the cached row JSON changes shape incompatibly. */
private const val PEN_VISIT_CACHE_SHAPE = "visit-v1"

/**
 * The whole-page facts the backend composes beside the rows on every list refresh: its title,
 * the filter chips (labels, counts, empty copy) and the caller's open count. Rendered verbatim;
 * the client derives none of it.
 */
data class PenVisitPageMeta(
    val title: String = "",
    val filters: List<PenVisitFilterDto> = emptyList(),
    val openCount: Int = 0,
)

/**
 * Pen visits (maintainer decision 2026-09-07). READS are offline-first per
 * docs/decisions/android-offline-first.md: the list and the detail both render from Room and
 * network refreshes upsert Room. The WRITE — the visit's one video — does not live here: it
 * rides the durable outbox through `SyncRepository.enqueuePenVisitSubmit`, and the sync engine
 * reconciles the successful submit's returned task back through [persistServerDetail].
 */
interface PenVisitsRepository {
    /** The paged visit list for one backend filter KEY ("" = the backend default). */
    fun visits(filter: String): Flow<PagingData<PenVisitDto>>

    /** Backend-composed page facts from the LAST list refresh. */
    val pageMeta: StateFlow<PenVisitPageMeta>

    /** Drops one scope's freshness marker so the next pager refetches instead of TTL-skipping. */
    suspend fun invalidateVisits(filter: String)

    /** Room-first visit detail; null while nothing is cached yet. */
    fun observeVisit(taskId: String): Flow<PenVisitDto?>

    /** Network -> Room detail refresh. Non-blocking contract: a failure leaves the cache serving. */
    suspend fun refreshVisit(taskId: String)

    /**
     * Reconciles a successful submit's RETURNED task into Room — the detail cache AND every
     * cached list-row copy — and drops every list scope's freshness marker (a done visit moves
     * from the To do chip to the Done chip). Called by the sync engine only.
     */
    suspend fun persistServerDetail(detail: PenVisitDetailDto)
}

class DefaultPenVisitsRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : PenVisitsRepository {

    private val _pageMeta = MutableStateFlow(PenVisitPageMeta())
    override val pageMeta: StateFlow<PenVisitPageMeta> = _pageMeta

    @OptIn(ExperimentalPagingApi::class)
    override fun visits(filter: String): Flow<PagingData<PenVisitDto>> {
        val key = scopeKey(filter)
        return Pager(
            config = PagingConfig(
                pageSize = PEN_VISIT_PAGE_SIZE,
                initialLoadSize = PEN_VISIT_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = PEN_VISIT_PAGE_SIZE * 3,
            ),
            remoteMediator = PenVisitRemoteMediator(
                filter = filter,
                api = api,
                database = database,
                json = json,
                clock = clock,
                onMeta = { meta -> _pageMeta.value = meta },
            ),
            pagingSourceFactory = { database.penVisitItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<PenVisitDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateVisits(filter: String) {
        database.penVisitRemoteKeyDao().delete(scopeKey(filter))
    }

    override fun observeVisit(taskId: String): Flow<PenVisitDto?> =
        database.penVisitDetailCacheDao().observe(taskId)
            .map { entity ->
                readCachedJson<PenVisitDto>(
                    json = json,
                    cacheKey = taskId,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.penVisitDetailCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshVisit(taskId: String) {
        // exception:exempt expected refresh failure (offline/timeout/5xx); the cache keeps serving
        // and the next successful open/refresh repairs it — the non-blocking refresh contract.
        runCatching { persistServerDetail(api.getPenVisit(taskId), invalidateLists = false) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "pen_visit_detail_refresh_failed task=$taskId", it)
            }
    }

    override suspend fun persistServerDetail(detail: PenVisitDetailDto) {
        persistServerDetail(detail, invalidateLists = true)
    }

    private suspend fun persistServerDetail(detail: PenVisitDetailDto, invalidateLists: Boolean) {
        val task = detail.task
        if (task.taskId.isBlank()) return
        val now = clock()
        val detailDao = database.penVisitDetailCacheDao()
        val itemDao = database.penVisitItemDao()
        val taskJson = json.encodeToString(task)
        database.withTransaction {
            // MONOTONIC on the server's own row_version: a payload OLDER than what Room already
            // holds is never written back. The sync engine replays a succeeded write's stored
            // response after a restart, and that response is the task as it was when the write
            // landed; if the screen has since refreshed a newer row, the replay must lose.
            val cached = detailDao.get(task.taskId)?.dtoJson
                ?.let { runCatching { json.decodeFromString<PenVisitDto>(it) }.getOrNull() }
            if (cached != null && cached.rowVersion > task.rowVersion) {
                android.util.Log.w(
                    LOG_TAG,
                    "pen_visit_detail_stale_ignored task=${task.taskId} cached_rv=${cached.rowVersion} incoming_rv=${task.rowVersion}",
                )
                return@withTransaction
            }
            detailDao.upsert(PenVisitDetailCacheEntity(cacheKey = task.taskId, dtoJson = taskJson, updatedAt = now))
            // Every cached list-row copy adopts the server's fresh task so a re-entered list shows
            // the new chip without a refetch.
            itemDao.upsertAll(itemDao.rowsForTask(task.taskId).map { row -> row.copy(dtoJson = taskJson, updatedAt = now) })
            if (invalidateLists) {
                // A submit moves the visit between chips; dropping the markers makes the next
                // pager refresh refetch every scope while the cached rows keep serving.
                database.penVisitRemoteKeyDao().deleteAll()
            }
        }
        detailDao.enforceCacheBounds()
    }

    private fun scopeKey(filter: String): String =
        cacheKey(PEN_VISIT_CACHE_SHAPE, "pen-visits", filter, PEN_VISIT_PAGE_SIZE.toString())

    private companion object {
        const val LOG_TAG = "GoatOsPenVisits"
    }
}

/**
 * Fills Room from `GET /app/pen-visits` page-by-page over the endpoint's opaque keyset cursor.
 * Room stays the single source of truth: this never hands rows to the UI, it only writes them,
 * and the [androidx.paging.PagingSource] re-emits. ALWAYS refresh on open: the kernel raises a
 * visit the morning after the work, so a list cached last night is stale by definition.
 */
@OptIn(ExperimentalPagingApi::class)
private class PenVisitRemoteMediator(
    private val filter: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val onMeta: (PenVisitPageMeta) -> Unit,
) : RemoteMediator<Int, PenVisitItemEntity>() {
    private val queryKey = cacheKey(PEN_VISIT_CACHE_SHAPE, "pen-visits", filter, PEN_VISIT_PAGE_SIZE.toString())

    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, PenVisitItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.penVisitRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = api.getPenVisits(
                filter = filter.ifBlank { null },
                limit = PEN_VISIT_PAGE_SIZE,
                cursor = cursor,
            )
            if (loadType == LoadType.REFRESH) {
                // Page facts ride the refresh only: counts are whole-list, so an APPEND page
                // fetched long after the user last looked must not overwrite them.
                onMeta(PenVisitPageMeta(title = response.title, filters = response.filters, openCount = response.openCount))
            }
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            // A cursor that did not ADVANCE is also the end, or an echoing backend would spin
            // this mediator forever on one page.
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            database.withTransaction {
                val itemDao = database.penVisitItemDao()
                val remoteKeyDao = database.penVisitRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.rows.mapIndexed { index, visit ->
                        PenVisitItemEntity(
                            queryKey = queryKey,
                            grainKey = visit.taskId,
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(visit),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    PenVisitRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor.orEmpty(),
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteRowsOutsideNewestQueries(PEN_VISIT_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(PEN_VISIT_CACHED_QUERIES)
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
