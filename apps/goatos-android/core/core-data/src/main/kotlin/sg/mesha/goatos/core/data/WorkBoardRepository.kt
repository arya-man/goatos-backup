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
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.CacheGovernance
import sg.mesha.goatos.core.data.cache.WorkBoardItemEntity
import sg.mesha.goatos.core.data.cache.WorkBoardMetaCacheDao
import sg.mesha.goatos.core.data.cache.WorkBoardMetaCacheEntity
import sg.mesha.goatos.core.data.cache.WorkBoardRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.WorkBoardRowDto
import sg.mesha.goatos.core.network.dto.WorkBoardRowsPageDto
import sg.mesha.goatos.core.network.dto.WorkBoardSummaryDto

/**
 * One screen-page of board rows. A phone viewport holds ~7-10 rows; anything larger is the mobile
 * twin of compute-on-read (docs/decisions/mobile-data-fetch-anti-patterns.md). Bounds BOTH the
 * network request and the Room window the UI observes.
 */
const val WORK_BOARD_PAGE_SIZE = 20

/** How many distinct filter scopes keep their cached rows, bounding the tables over months of use. */
private const val WORK_BOARD_CACHED_QUERIES = 6

/** Bump whenever the cached row/summary JSON changes shape incompatibly. */
private const val WORK_BOARD_CACHE_SHAPE = "board-v1"

/**
 * The board's four lanes in display order, and the work states each one holds. This MIRRORS
 * `backend/internal/workboard/domain.LaneFor` for ONE purpose only: turning a tapped lane chip into
 * the `state=` CSV the rows/summary endpoints accept (the API filters by state, not by lane). It is
 * never used to place a row in a column — every row carries its server-derived `lane` verbatim.
 */
object WorkBoardLanes {
    const val TODO = "todo"
    const val IN_PROGRESS = "in_progress"
    const val IN_REVIEW = "in_review"
    const val DONE = "done"

    val ORDER: List<String> = listOf(TODO, IN_PROGRESS, IN_REVIEW, DONE)

    /** The states a lane filter requests. `todo` is the backend's default branch. */
    fun statesFor(lane: String): List<String> = when (lane) {
        IN_PROGRESS -> listOf("in_progress", "proof_pending", "rejected", "blocked")
        IN_REVIEW -> listOf("verification_pending")
        DONE -> listOf("completed")
        TODO -> listOf("scheduled", "due", "overdue", "deferred", "missed")
        else -> emptyList()
    }
}

/**
 * Filter scope for the board. Every field is a backend query parameter; [roomKey] partitions the
 * cached rows, the cursor and the summary envelope, so changing a filter can never mix two scopes.
 *
 * [parkId] may be null: the backend resolves a park-scoped caller's own park, and only a
 * tenant-wide caller must name one (`park_required` otherwise). [businessDate] is the IST day.
 */
data class WorkBoardQuery(
    val parkId: String?,
    val businessDate: String,
    /** A lane key from [WorkBoardLanes], or "" for every lane. */
    val lane: String = "",
    /** A module key, or "" for every module the caller may see. */
    val module: String = "",
    /** `me`, a user id, or "" for everyone in scope (ignored server-side without oversee). */
    val owner: String = "",
    /** Forces a network refresh without partitioning the stable Room cache scope. */
    val refreshNonce: Int = 0,
) {
    val stateCsv: String? get() = WorkBoardLanes.statesFor(lane).joinToString(",").ifBlank { null }
    val moduleCsv: String? get() = module.ifBlank { null }
    val ownerParam: String? get() = owner.ifBlank { null }

    /**
     * The scope the SUMMARY is read for: the whole board of this park/day/owner, with no lane or
     * module narrowing. The tiles and every chip count describe the board, and a lane chip's count
     * must survive selecting another lane — a summary narrowed to `in_review` would read every
     * other lane as 0.
     */
    fun summaryQuery(): WorkBoardQuery = copy(lane = "", module = "", refreshNonce = 0)

    fun summaryKey(): String = summaryQuery().roomKey()

    fun roomKey(): String = cacheKey(
        WORK_BOARD_CACHE_SHAPE,
        parkId,
        businessDate,
        lane,
        module,
        owner,
        WORK_BOARD_PAGE_SIZE.toString(),
    )
}

/**
 * The Work Board's My Work reads (maintainer decision 2026-09-10). Offline-first per
 * docs/decisions/android-offline-first.md: the summary tiles render from the `work_board_meta_cache`
 * blob and the rows from a Room [androidx.paging.PagingSource] filled page-by-page over the
 * backend's STRING keyset cursor by a `RemoteMediator`. Both layers page at [WORK_BOARD_PAGE_SIZE];
 * nothing ever materialises a whole board.
 */
interface WorkBoardRepository {
    /** The WHOLE-FILTER summary for the scope, Room-first; `data == null` on a cold cache. */
    fun observeSummary(query: WorkBoardQuery): Flow<Resource<WorkBoardSummaryDto>>

    /** The paged rows, a Room PagingSource filled by a RemoteMediator. */
    fun observeRows(query: WorkBoardQuery): Flow<PagingData<WorkBoardRowDto>>

    /**
     * Network -> Room summary refresh. Upserts on success; a failure leaves the cache UNTOUCHED
     * and is returned so the caller can mark the screen offline. Non-blocking contract.
     */
    suspend fun refresh(query: WorkBoardQuery): Result<Unit>

    /**
     * ONE cached board row by its `row_key`, across any scope this device has paged — the L1
     * detail's read. Null while nothing is cached; never a second network call.
     */
    fun observeRow(rowKey: String): Flow<WorkBoardRowDto?>
}

class DefaultWorkBoardRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val metaDao: WorkBoardMetaCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : WorkBoardRepository {

    override fun observeSummary(query: WorkBoardQuery): Flow<Resource<WorkBoardSummaryDto>> {
        val key = query.summaryKey()
        return metaDao.observe(key)
            .map { entity ->
                val cached = readCachedJson<WorkBoardSummaryDto>(
                    json = json,
                    cacheKey = key,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { metaDao.delete(it) },
                )
                Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
            }
            .flowOn(Dispatchers.Default)
    }

    @OptIn(ExperimentalPagingApi::class)
    override fun observeRows(query: WorkBoardQuery): Flow<PagingData<WorkBoardRowDto>> {
        val key = query.roomKey()
        return Pager(
            config = PagingConfig(
                pageSize = WORK_BOARD_PAGE_SIZE,
                initialLoadSize = WORK_BOARD_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = WORK_BOARD_PAGE_SIZE * 3,
            ),
            remoteMediator = WorkBoardRemoteMediator(query, api, database, metaDao, json, clock),
            pagingSourceFactory = { database.workBoardItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<WorkBoardRowDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refresh(query: WorkBoardQuery): Result<Unit> {
        val key = query.summaryKey()
        val scope = query.summaryQuery()
        return try {
            val summary = api.getWorkBoardSummary(
                park = scope.parkId,
                businessDate = scope.businessDate,
                module = scope.moduleCsv,
                state = scope.stateCsv,
                owner = scope.ownerParam,
            )
            metaDao.upsert(
                WorkBoardMetaCacheEntity(cacheKey = key, dtoJson = json.encodeToString(summary), updatedAt = clock()),
            )
            metaDao.enforceCacheBounds()
            Result.success(Unit)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            // Room keeps whatever it already had: the screen renders the cached summary behind
            // an offline indicator instead of a blank wall.
            Result.failure(error)
        }
    }

    override fun observeRow(rowKey: String): Flow<WorkBoardRowDto?> =
        database.workBoardItemDao().observeRow(rowKey)
            .map { entity ->
                entity?.let {
                    runCatching { json.decodeFromString<WorkBoardRowDto>(it.dtoJson) }
                        .onFailure { error -> android.util.Log.w(LOG_TAG, "work_board_row_decode_failed row=$rowKey", error) }
                        .getOrNull()
                }
            }
            .flowOn(Dispatchers.Default)

    private companion object {
        const val LOG_TAG = "GoatOsWorkBoard"
    }
}

/**
 * Fills Room from `GET /work-board/rows` page-by-page over the endpoint's opaque keyset cursor.
 * A REFRESH also lands the board-wide `/work-board/summary` ([WorkBoardQuery.summaryQuery]) in the
 * SAME transaction as its first page, so the tiles and the rows a reader sees together were served
 * together.
 */
@OptIn(ExperimentalPagingApi::class)
private class WorkBoardRemoteMediator(
    private val query: WorkBoardQuery,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val metaDao: WorkBoardMetaCacheDao,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, WorkBoardItemEntity>() {
    private val queryKey = query.roomKey()

    override suspend fun initialize(): InitializeAction {
        if (query.refreshNonce > 0) return InitializeAction.LAUNCH_INITIAL_REFRESH
        val cachedAt = database.workBoardRemoteKeyDao().get(queryKey)?.updatedAt
        return if (cachedAt != null && clock() - cachedAt < CacheGovernance.DEFAULT_TTL_MILLIS) {
            InitializeAction.SKIP_INITIAL_REFRESH
        } else {
            InitializeAction.LAUNCH_INITIAL_REFRESH
        }
    }

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, WorkBoardItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.workBoardRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isNullOrBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response: WorkBoardRowsPageDto = api.getWorkBoardRows(
                park = query.parkId,
                businessDate = query.businessDate,
                module = query.moduleCsv,
                state = query.stateCsv,
                owner = query.ownerParam,
                limit = WORK_BOARD_PAGE_SIZE,
                cursor = cursor,
            )
            // The summary rides the REFRESH only: its counts are whole-filter, so an APPEND page
            // fetched long after the reader last looked must not overwrite them.
            val summary: WorkBoardSummaryDto? = if (loadType == LoadType.REFRESH) {
                val scope = query.summaryQuery()
                api.getWorkBoardSummary(
                    park = scope.parkId,
                    businessDate = scope.businessDate,
                    module = scope.moduleCsv,
                    state = scope.stateCsv,
                    owner = scope.ownerParam,
                )
            } else {
                null
            }
            // An absent next_cursor is the contract's own end-of-pages signal; a cursor that did
            // not ADVANCE is also the end, or an echoing backend would spin this mediator forever.
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            // Page rows and their cursor commit TOGETHER, or a crash between them leaves the
            // cursor pointing past rows that were never stored.
            database.withTransaction {
                val itemDao = database.workBoardItemDao()
                val remoteKeyDao = database.workBoardRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.rows.mapIndexed { index, row ->
                        WorkBoardItemEntity(
                            queryKey = queryKey,
                            rowKey = row.rowKey,
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(row),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    WorkBoardRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (summary != null) {
                    metaDao.upsert(
                        WorkBoardMetaCacheEntity(
                            cacheKey = query.summaryKey(),
                            dtoJson = json.encodeToString(summary),
                            updatedAt = updatedAt,
                        ),
                    )
                    itemDao.deleteRowsOutsideNewestQueries(WORK_BOARD_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(WORK_BOARD_CACHED_QUERIES)
                }
            }
            if (summary != null) metaDao.enforceCacheBounds()
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            // Room keeps whatever it already had: a failed page load surfaces as a Paging
            // LoadState.Error the screen renders beside the cached rows, never as a wipe.
            MediatorResult.Error(error)
        }
    }
}
