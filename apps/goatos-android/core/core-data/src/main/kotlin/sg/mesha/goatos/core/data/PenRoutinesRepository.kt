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
import kotlinx.coroutines.flow.distinctUntilChanged
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
import sg.mesha.goatos.core.network.dto.PenRoutinePenOptionDto
import sg.mesha.goatos.core.network.dto.PenRoutineTabDto

/** One screen-page of routine tasks — bounds BOTH the network request and the Room window
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val PEN_ROUTINE_PAGE_SIZE = 20

/**
 * How many distinct list scopes keep their cached rows. A scope is one tab (the Routines list or a
 * web-authored phone tab) x one filter selection, so this holds a couple of tabs warm at once.
 */
private const val PEN_ROUTINE_CACHED_QUERIES = 8

/** How many tabs keep their page facts (title, chips, pen options) in memory. */
private const val PEN_ROUTINE_META_TABS = 8

/**
 * One list request. [tab] blank = the Routines list; a key = a web-authored phone tab (maintainer
 * instruction 2026-10-01). [filter] is the backend status KEY ("" = backend default), [dueFrom] /
 * [dueTo] an inclusive ISO business-date window ("" = no narrowing), [pen] a `pen_options[].value`
 * token ("" = every pen). Every field is part of the Room cache key, so two tabs -- or two filter
 * selections on one tab -- never read each other's rows.
 */
data class PenRoutineQuery(
    val filter: String = "",
    val tab: String = "",
    val dueFrom: String = "",
    val dueTo: String = "",
    val pen: String = "",
)

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
    /** The web-authored tab the page was opened from; null on the Routines list. */
    val tab: PenRoutineTabDto? = null,
    /** The pens the tab's Pen filter offers (empty unless it offers one). */
    val penOptions: List<PenRoutinePenOptionDto> = emptyList(),
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
    /** The paged task list for one [PenRoutineQuery] (a tab and its filter selection). */
    fun tasks(query: PenRoutineQuery): Flow<PagingData<PenRoutineTaskDto>>

    /**
     * Backend-composed page facts from the LAST list refresh of ONE tab ("" = the Routines list),
     * so a web-authored tab never shows the Routines title or another tab's chips.
     */
    fun pageMeta(tab: String): Flow<PenRoutinePageMeta>

    /** Drops one scope's freshness marker so the next pager refetches instead of TTL-skipping. */
    suspend fun invalidateTasks(query: PenRoutineQuery)

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

    /** Page facts per tab key, bounded to [PEN_ROUTINE_META_TABS] (oldest refresh dropped first). */
    private val metaByTab = MutableStateFlow<Map<String, PenRoutinePageMeta>>(emptyMap())

    override fun pageMeta(tab: String): Flow<PenRoutinePageMeta> =
        metaByTab.map { it[tab] ?: PenRoutinePageMeta() }.distinctUntilChanged()

    private fun publishMeta(tab: String, meta: PenRoutinePageMeta) {
        val next = LinkedHashMap(metaByTab.value)
        next.remove(tab)
        next[tab] = meta
        while (next.size > PEN_ROUTINE_META_TABS) next.remove(next.keys.first())
        metaByTab.value = next
    }

    @OptIn(ExperimentalPagingApi::class)
    override fun tasks(query: PenRoutineQuery): Flow<PagingData<PenRoutineTaskDto>> {
        val key = penRoutineScopeKey(query)
        return Pager(
            config = PagingConfig(
                pageSize = PEN_ROUTINE_PAGE_SIZE,
                initialLoadSize = PEN_ROUTINE_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = PEN_ROUTINE_PAGE_SIZE * 3,
            ),
            remoteMediator = PenRoutineRemoteMediator(
                query = query,
                api = api,
                database = database,
                json = json,
                clock = clock,
                onMeta = { meta -> publishMeta(query.tab, meta) },
            ),
            pagingSourceFactory = { database.penRoutineItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<PenRoutineTaskDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateTasks(query: PenRoutineQuery) {
        database.penRoutineRemoteKeyDao().delete(penRoutineScopeKey(query))
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
    private val query: PenRoutineQuery,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val onMeta: (PenRoutinePageMeta) -> Unit,
) : RemoteMediator<Int, PenRoutineItemEntity>() {
    private val queryKey = penRoutineScopeKey(query)

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
                filter = query.filter.ifBlank { null },
                limit = PEN_ROUTINE_PAGE_SIZE,
                cursor = cursor,
                tab = query.tab.ifBlank { null },
                dueFrom = query.dueFrom.ifBlank { null },
                dueTo = query.dueTo.ifBlank { null },
                pen = query.pen.ifBlank { null },
            )
            if (loadType == LoadType.REFRESH) {
                // Page facts ride the refresh only: counts are whole-list, so an APPEND page
                // fetched long after the user last looked must not overwrite them.
                onMeta(
                    PenRoutinePageMeta(
                        title = response.title,
                        filters = response.filters,
                        openCount = response.openCount,
                        tab = response.tab,
                        penOptions = response.penOptions,
                    ),
                )
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

/**
 * The Room scope key of one list request. The Routines list with no narrowing keeps the key it
 * always had, byte for byte, so an upgrade does not orphan its cached rows; a tab or a date/pen
 * narrowing appends labelled segments so no two scopes can collide.
 */
internal fun penRoutineScopeKey(query: PenRoutineQuery): String {
    val base = listOf(PEN_ROUTINE_CACHE_SHAPE, "pen-routines", query.filter, PEN_ROUTINE_PAGE_SIZE.toString())
    val narrowed = query.tab.isNotBlank() || query.dueFrom.isNotBlank() || query.dueTo.isNotBlank() || query.pen.isNotBlank()
    if (!narrowed) return cacheKey(*base.toTypedArray())
    return cacheKey(
        *(base + listOf("tab=${query.tab}", "from=${query.dueFrom}", "to=${query.dueTo}", "pen=${query.pen}")).toTypedArray(),
    )
}
