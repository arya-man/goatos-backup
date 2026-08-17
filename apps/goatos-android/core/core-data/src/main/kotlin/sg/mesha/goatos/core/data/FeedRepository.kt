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
import sg.mesha.goatos.core.data.cache.FeedDirectionItemEntity
import sg.mesha.goatos.core.data.cache.FeedDirectionMetaCacheDao
import sg.mesha.goatos.core.data.cache.FeedDirectionMetaCacheEntity
import sg.mesha.goatos.core.data.cache.FeedDirectionRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.FeedPackingItemEntity
import sg.mesha.goatos.core.data.cache.FeedPackingMetaCacheDao
import sg.mesha.goatos.core.data.cache.FeedPackingMetaCacheEntity
import sg.mesha.goatos.core.data.cache.FeedPackingRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCapturedSlotDto
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingWorklistPageDto

/**
 * One screen-page of feed rows. A phone viewport holds ~7-10 rows; anything larger is the mobile
 * twin of compute-on-read (docs/decisions/mobile-data-fetch-anti-patterns.md). This bounds BOTH the
 * network request and the Room window the UI observes.
 */
const val FEED_PAGE_SIZE = 20

/** How many distinct filter scopes keep their cached rows, bounding the tables over months of use. */
private const val FEED_CACHED_QUERIES = 8

/**
 * Filter scope for the Feed Direction sheet. Every field is a backend query parameter; [roomKey]
 * partitions cached rows, the page offset, and the summary envelope, so changing a filter can never
 * mix two scopes' rows together. [parkId] and [targetDate] are REQUIRED by the backend.
 */
data class FeedDirectionQuery(
    val parkId: String,
    val targetDate: String,
    val shedId: String? = null,
    val session: Int? = null,
    val workflow: String? = null,
    // Verification-lifecycle filter (pending | pending_verification | completed); null = every status.
    // Part of roomKey so a status change caches its own page/summary, never mixing two status scopes.
    val status: String? = null,
    // Cache-only refresh namespace. The backend has no refresh_nonce parameter; this only forces a
    // new Room/Paging scope so manual refresh/resume cannot keep serving a fresh-but-stale cache.
    val refreshNonce: Int = 0,
) {
    fun roomKey(): String = cacheKey(
        DIRECTION_CACHE_SHAPE,
        parkId,
        targetDate,
        shedId,
        session?.toString(),
        workflow,
        status,
        refreshNonce.toString(),
        FEED_PAGE_SIZE.toString(),
    )
}

/** Filter scope for the Feed Packing worklist. */
data class FeedPackingQuery(
    val parkId: String,
    val targetDate: String,
    val session: Int? = null,
    val workflow: String? = null,
    val status: String? = null,
) {
    /**
     * The cache namespace for this scope.
     *
     * [PACKING_CACHE_SHAPE] is part of the key so rows cached by an EARLIER app version can never be
     * read back into the current DTO. This has now mattered twice in opposite directions: the pen-day
     * build stored `sessions` where this one stores `items`, and vice versa. Either way the stale JSON
     * deserializes WITHOUT ERROR into a card with no feed lines at all, because
     * kotlinx-serialization fills the missing field with its default empty list -- a packer would
     * open the app to a pen with nothing to pack. Bumping the namespace orphans those rows instead,
     * and the existing newest-queries eviction reclaims the space.
     */
    fun roomKey(): String =
        cacheKey(PACKING_CACHE_SHAPE, parkId, targetDate, session?.toString(), workflow, status, FEED_PAGE_SIZE.toString())
}

/**
 * Bump whenever the cached packing row JSON changes shape incompatibly.
 *
 * v3 = back to one row per shed-SESSION with a flat `items` list (maintainer decision 2026-08-11).
 * Never REUSE an old value when reverting to an old shape: `session-v1` rows may still be sitting in
 * a phone's cache from before the pen-day build, and they are not guaranteed to match today's DTO in
 * every other field.
 */
private const val PACKING_CACHE_SHAPE = "session-v3"

/**
 * Bump whenever the cached direction row JSON or cache semantics change incompatibly.
 *
 * v2 = refresh/resume has its own cache namespace via FeedDirectionQuery.refreshNonce, so phones do
 * not keep reading an old fresh Room page after STG/API has gained a missing shed-partition row.
 */
private const val DIRECTION_CACHE_SHAPE = "direction-v2"

/**
 * Feed vertical reads: the generated Feed Direction sheet and the Feed Packing worklist.
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of truth.
 * `observe*Totals` is a cache-first reactive stream of the whole-scope SUMMARY (never re-derived by
 * summing paged rows); `*Rows` is a Room [androidx.paging.PagingSource] filled page-by-page from the
 * backend by a `RemoteMediator`. Both layers page identically at [FEED_PAGE_SIZE]; nothing ever
 * holds the whole row set. These are read-only surfaces — there is no capture/write path here.
 */
interface FeedRepository {
    /** Cache-first stream of the Feed Direction whole-scope summary (`total_kg_by_feed_item`,
     *  blocked counts). Independent of the page. */
    fun observeDirectionTotals(query: FeedDirectionQuery): Flow<Resource<FeedDirectionPreviewPageDto>>

    /** The paged Feed Direction rows, a Room PagingSource filled by a RemoteMediator. */
    fun directionRows(query: FeedDirectionQuery): Flow<PagingData<FeedDirectionRowDto>>

    /** Cache-first stream of the Feed Packing whole-scope summary. */
    fun observePackingTotals(query: FeedPackingQuery): Flow<Resource<FeedPackingWorklistPageDto>>

    /** The paged Feed Packing lines, a Room PagingSource filled by a RemoteMediator. */
    fun packingRows(query: FeedPackingQuery): Flow<PagingData<FeedPackingRowDto>>

    /**
     * Which of ONE pen-session's proof slots are already recorded, by ANY operator.
     *
     * Deliberately NOT cache-first: this answers "has someone else done this slot in the last few
     * minutes", and a stale cached answer is worse than none — it would either hide work that was
     * just done or claim work that was withdrawn. A failure returns an empty list, so the capture
     * screen degrades to exactly its pre-2026-08-14 single-phone behaviour rather than breaking.
     */
    suspend fun penSessionCaptures(query: FeedPenSessionCaptureQuery): List<FeedDistributionCapturedSlotDto>
}

/** Addresses ONE pen-session. [partitionLabel] is identity, not decoration. */
data class FeedPenSessionCaptureQuery(
    val parkId: String,
    val shedId: String,
    val partitionLabel: String,
    val sessionNo: Int,
    val targetDate: String,
    val workflow: String,
)

class DefaultFeedRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val directionMetaDao: FeedDirectionMetaCacheDao,
    private val packingMetaDao: FeedPackingMetaCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : FeedRepository {

    override fun observeDirectionTotals(
        query: FeedDirectionQuery,
    ): Flow<Resource<FeedDirectionPreviewPageDto>> {
        val key = query.roomKey()
        return directionMetaDao.observe(key)
            .map { entity ->
                val cached = readCachedJson<FeedDirectionPreviewPageDto>(
                    json = json,
                    cacheKey = key,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { directionMetaDao.delete(it) },
                )
                Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
            }
            .flowOn(Dispatchers.Default)
    }

    @OptIn(ExperimentalPagingApi::class)
    override fun directionRows(query: FeedDirectionQuery): Flow<PagingData<FeedDirectionRowDto>> {
        val key = query.roomKey()
        return Pager(
            config = PagingConfig(
                pageSize = FEED_PAGE_SIZE,
                initialLoadSize = FEED_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = FEED_PAGE_SIZE * 3,
            ),
            remoteMediator = FeedDirectionRemoteMediator(query, api, database, directionMetaDao, json, clock),
            pagingSourceFactory = { database.feedDirectionItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<FeedDirectionRowDto>(entity.dtoJson) } }
            .flowOn(Dispatchers.Default)
    }

    override fun observePackingTotals(
        query: FeedPackingQuery,
    ): Flow<Resource<FeedPackingWorklistPageDto>> {
        val key = query.roomKey()
        return packingMetaDao.observe(key)
            .map { entity ->
                val cached = readCachedJson<FeedPackingWorklistPageDto>(
                    json = json,
                    cacheKey = key,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { packingMetaDao.delete(it) },
                )
                Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
            }
            .flowOn(Dispatchers.Default)
    }

    @OptIn(ExperimentalPagingApi::class)
    override fun packingRows(query: FeedPackingQuery): Flow<PagingData<FeedPackingRowDto>> {
        val key = query.roomKey()
        return Pager(
            config = PagingConfig(
                pageSize = FEED_PAGE_SIZE,
                initialLoadSize = FEED_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = FEED_PAGE_SIZE * 3,
            ),
            remoteMediator = FeedPackingRemoteMediator(query, api, database, packingMetaDao, json, clock),
            pagingSourceFactory = { database.feedPackingItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<FeedPackingRowDto>(entity.dtoJson) } }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun penSessionCaptures( // offline-first-guard:ignore: liveness beats staleness here - a cached "someone already did this slot" would either hide work just done or claim work since withdrawn, and this only ADDS to a screen whose own capture state is already Room-backed.
        query: FeedPenSessionCaptureQuery,
    ): List<FeedDistributionCapturedSlotDto> =
        runCatching {
            api.getFeedDistributionCaptures(
                parkId = query.parkId.takeIf { it.isNotBlank() },
                shedId = query.shedId,
                partitionLabel = query.partitionLabel.takeIf { it.isNotBlank() },
                sessionNo = query.sessionNo,
                targetDate = query.targetDate,
                workflow = query.workflow,
            ).items
        }.getOrElse {
            // Fail SOFT and silent. This read only ADDS knowledge of other operators' work; without
            // it the screen behaves exactly as it did before the read existed. Surfacing an error
            // here would put a failure banner on a screen whose own capture state is perfectly fine.
            emptyList()
        }
}

/**
 * Fills Room from `/feed-direction/preview` page-by-page. Room stays the single source of truth:
 * this only writes rows; the [androidx.paging.PagingSource] re-emits. The offset is persisted per
 * scope so an APPEND after process death resumes at the right page, and it always advances by the
 * number of rows actually returned — a short page ends pagination rather than looping.
 */
@OptIn(ExperimentalPagingApi::class)
private class FeedDirectionRemoteMediator(
    private val query: FeedDirectionQuery,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val metaDao: FeedDirectionMetaCacheDao,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, FeedDirectionItemEntity>() {
    private val queryKey = query.roomKey()

    override suspend fun initialize(): InitializeAction {
        val cachedAt = database.feedDirectionRemoteKeyDao().get(queryKey)?.updatedAt
        return if (cachedAt != null && clock() - cachedAt < CacheGovernance.DEFAULT_TTL_MILLIS) {
            InitializeAction.SKIP_INITIAL_REFRESH
        } else {
            InitializeAction.LAUNCH_INITIAL_REFRESH
        }
    }

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, FeedDirectionItemEntity>,
    ): MediatorResult {
        val offset = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> 0
            LoadType.APPEND -> {
                val remoteKey = database.feedDirectionRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached) return MediatorResult.Success(endOfPaginationReached = true)
                remoteKey.nextOffset
            }
        }
        return try {
            val response = api.getFeedDirectionPreview(
                parkId = query.parkId,
                targetDate = query.targetDate,
                shedId = query.shedId,
                session = query.session,
                workflow = query.workflow,
                status = query.status,
                limit = FEED_PAGE_SIZE,
                offset = offset,
            )
            // The backend pages the SHED set (limit/offset count SHEDS, not rows), and every shed
            // yields multiple rows (one per session x breed). So the next page's shed offset advances
            // by the number of DISTINCT sheds returned — NOT by row count, which would skip
            // (rows-per-shed - 1) x pageSize sheds each page and hide whole sheds. End-of-pagination
            // comes from the backend's has_more, not a short row page (a full shed page is > pageSize rows).
            val shedsReturned = response.items.map { it.shedId }.distinct().size
            val endReached = !response.hasMore
            val updatedAt = clock()
            database.withTransaction {
                val itemDao = database.feedDirectionItemDao()
                val remoteKeyDao = database.feedDirectionRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                // sortIndex must be a monotonic ROW cursor across pages; the shed offset cannot serve
                // as it (shed offset + row index would overlap the previous page's rows).
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.items.mapIndexed { index, row ->
                        FeedDirectionItemEntity(
                            queryKey = queryKey,
                            grainKey = row.grainKey,
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(row),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    FeedDirectionRemoteKeyEntity(
                        queryKey = queryKey,
                        nextOffset = offset + shedsReturned,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    metaDao.upsert(
                        FeedDirectionMetaCacheEntity(
                            cacheKey = queryKey,
                            // Summary envelope only: the whole-scope totals/blocked counts are the
                            // authoritative KPIs. Rows are dropped here — they live as normalized
                            // Room rows so this blob can never grow with the sheet.
                            dtoJson = json.encodeToString(response.copy(items = emptyList())),
                            updatedAt = updatedAt,
                        ),
                    )
                    itemDao.deleteRowsOutsideNewestQueries(FEED_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(FEED_CACHED_QUERIES)
                }
            }
            if (loadType == LoadType.REFRESH) metaDao.enforceCacheBounds()
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            MediatorResult.Error(error)
        }
    }
}

/** Fills Room from `/feed-packing/worklist` page-by-page. Mirror of [FeedDirectionRemoteMediator]. */
@OptIn(ExperimentalPagingApi::class)
private class FeedPackingRemoteMediator(
    private val query: FeedPackingQuery,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val metaDao: FeedPackingMetaCacheDao,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, FeedPackingItemEntity>() {
    private val queryKey = query.roomKey()

    override suspend fun initialize(): InitializeAction {
        val cachedAt = database.feedPackingRemoteKeyDao().get(queryKey)?.updatedAt
        return if (cachedAt != null && clock() - cachedAt < CacheGovernance.DEFAULT_TTL_MILLIS) {
            InitializeAction.SKIP_INITIAL_REFRESH
        } else {
            InitializeAction.LAUNCH_INITIAL_REFRESH
        }
    }

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, FeedPackingItemEntity>,
    ): MediatorResult {
        val offset = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> 0
            LoadType.APPEND -> {
                val remoteKey = database.feedPackingRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached) return MediatorResult.Success(endOfPaginationReached = true)
                remoteKey.nextOffset
            }
        }
        return try {
            val response = api.getFeedPackingWorklist(
                parkId = query.parkId,
                targetDate = query.targetDate,
                session = query.session,
                workflow = query.workflow,
                status = query.status,
                limit = FEED_PAGE_SIZE,
                offset = offset,
            )
            // Backend pages the SHED set; advance by DISTINCT sheds (not rows), end on has_more.
            // See FeedDirectionRemoteMediator for the full rationale.
            val shedsReturned = response.items.map { it.shedId }.distinct().size
            val endReached = !response.hasMore
            val updatedAt = clock()
            database.withTransaction {
                val itemDao = database.feedPackingItemDao()
                val remoteKeyDao = database.feedPackingRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.items.mapIndexed { index, row ->
                        FeedPackingItemEntity(
                            queryKey = queryKey,
                            grainKey = row.grainKey,
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(row),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    FeedPackingRemoteKeyEntity(
                        queryKey = queryKey,
                        nextOffset = offset + shedsReturned,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    metaDao.upsert(
                        FeedPackingMetaCacheEntity(
                            cacheKey = queryKey,
                            dtoJson = json.encodeToString(response.copy(items = emptyList())),
                            updatedAt = updatedAt,
                        ),
                    )
                    itemDao.deleteRowsOutsideNewestQueries(FEED_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(FEED_CACHED_QUERIES)
                }
            }
            if (loadType == LoadType.REFRESH) metaDao.enforceCacheBounds()
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            MediatorResult.Error(error)
        }
    }
}
