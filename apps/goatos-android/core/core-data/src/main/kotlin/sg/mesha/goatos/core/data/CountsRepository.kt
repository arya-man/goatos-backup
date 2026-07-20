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
import sg.mesha.goatos.core.data.cache.CountsBreakdownItemEntity
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheDao
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheEntity
import sg.mesha.goatos.core.data.cache.CountsBreakdownRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.CountsShiftingDestinationsCacheDao
import sg.mesha.goatos.core.data.cache.CountsShiftingDestinationsCacheEntity
import sg.mesha.goatos.core.data.cache.HerdSummaryCacheDao
import sg.mesha.goatos.core.data.cache.HerdSummaryCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto

/**
 * One screen-page of breakdown grains. A phone viewport holds ~7-10 rows; anything larger is the
 * mobile twin of compute-on-read (docs/decisions/mobile-data-fetch-anti-patterns.md). This bounds
 * BOTH the network request and the Room window the UI observes.
 */
const val COUNTS_BREAKDOWN_PAGE_SIZE = 20

/**
 * One screen-page of animal-lookup matches for the shifting picker. Same bound as every other
 * mobile fetch: an operator resolving a tag needs the handful of matches that tag produces, never
 * a cohort-sized result set.
 */
const val COUNTS_ANIMAL_LOOKUP_PAGE_SIZE = 20

/**
 * The shifting destination catalog's single cache key. There is exactly one catalog per caller
 * scope, so unlike the filter-scoped caches this table holds one row.
 */
private const val SHIFTING_DESTINATIONS_CACHE_KEY = "shifting-destinations"

/**
 * How many distinct filter scopes keep their cached rows. Bounds the breakdown tables against an
 * install that cycles through many filter combinations over months of use.
 */
private const val COUNTS_BREAKDOWN_CACHED_QUERIES = 8

/**
 * Filter scope for the Counts breakdown. Every field is a backend-supported query parameter; the
 * scope's [roomKey] is what partitions cached rows, the page offset, and the totals envelope, so
 * changing a filter can never mix two scopes' rows together.
 */
data class CountsBreakdownQuery(
    val parkId: String? = null,
    val shedId: String? = null,
    val managementStage: String? = null,
    val breed: String? = null,
    val sex: String? = null,
    val lifecycleStatus: String? = null,
) {
    fun roomKey(): String = cacheKey(
        parkId,
        shedId,
        managementStage,
        breed,
        sex,
        lifecycleStatus,
        COUNTS_BREAKDOWN_PAGE_SIZE.toString(),
    )
}

/**
 * Counts vertical reads: the herd-register census rollup and the paged census breakdown.
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of
 * truth for both. `observeX` is cache-first and reactive — it emits immediately from Room (null
 * data only on a cold cache) and re-emits whenever a background refresh upserts. `refreshX` is
 * the network half of stale-while-revalidate: it upserts Room on success and leaves the cache
 * completely untouched on failure, so a screen shows cached rows plus an offline indicator rather
 * than a blank wall.
 *
 * The WRITE side of Counts (birth / death / shifting) does NOT live here — those go through the
 * durable outbox (`sync/SyncRepository`), never a direct API call from a ViewModel.
 */
interface CountsRepository {
    /** Cache-first stream of the scoped census rollup. */
    fun observeHerdSummary(
        lifecycleStatus: String? = null,
        parkId: String? = null,
        breed: String? = null,
        sex: String? = null,
    ): Flow<Resource<HerdRegisterSummaryResponseDto>>

    /** Fetches and upserts Room on success; on failure returns it and leaves the cache intact. */
    suspend fun refreshHerdSummary(
        lifecycleStatus: String? = null,
        parkId: String? = null,
        breed: String? = null,
        sex: String? = null,
    ): Result<Unit>

    /**
     * Cache-first stream of the breakdown's whole-result envelope (totals / charts / facets).
     * These are rolled up by the backend over the FULL filtered set and are independent of the
     * page — a KPI here is never re-derived by summing the rows currently paged into memory.
     */
    fun observeBreakdownTotals(query: CountsBreakdownQuery): Flow<Resource<CountsBreakdownResponseDto>>

    /**
     * The paged breakdown rows: a Room [androidx.paging.PagingSource] filled page-by-page from the
     * backend by a `RemoteMediator`. Both layers page identically at
     * [COUNTS_BREAKDOWN_PAGE_SIZE]; nothing ever holds the whole grain set.
     */
    fun breakdownRows(query: CountsBreakdownQuery): Flow<PagingData<CountsBreakdownRowDto>>

    /**
     * Cache-first stream of the shifting DESTINATION CATALOG (every park the caller may move
     * animals into, each with its sheds).
     *
     * Room-backed and observed as a Flow like every other screen-facing read
     * (docs/decisions/android-offline-first.md) — unlike [lookupAnimals], this one genuinely IS a
     * read model: it is a bounded, slow-moving vocabulary (order-of two parks, ~154 sheds) whose
     * staleness is harmless, because a park or shed that briefly disappears from the catalog is
     * still re-validated server-side at submit time. Serving it from cache is what lets the
     * dropdowns open with real options in a shed with no signal.
     */
    fun observeShiftingDestinations(): Flow<Resource<CountsShiftingDestinationsResponseDto>>

    /** Fetches and upserts Room on success; on failure returns it and leaves the cache intact. */
    suspend fun refreshShiftingDestinations(): Result<Unit>

    /**
     * One-shot animal lookup for the shifting screen's picker: resolves a scanned/typed tag to the
     * `goat_id` a shifting write must carry.
     *
     * Deliberately NOT Room-cached, and that is not an offline-first exemption by convenience.
     * This is not a screen read model — it is a live question ("which animal is this tag on, right
     * now") whose answer decides which real animals a movement will relocate. Serving it from a
     * stale local cache could name an animal that has since died, exited, or already moved, and
     * caching it properly would mean holding a searchable whole-herd index on the device — the
     * unbounded cache the mobile scale rules ban outright. A failed lookup surfaces as a retryable
     * error next to the field; it never silently returns an empty match.
     *
     * Bounded to one screen-page ([COUNTS_ANIMAL_LOOKUP_PAGE_SIZE]) like every other mobile fetch.
     */
    suspend fun lookupAnimals(
        query: String,
        parkId: String? = null,
        shedId: String? = null,
    ): Result<List<GoatSearchItemDto>>
}

class DefaultCountsRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val summaryDao: HerdSummaryCacheDao,
    private val breakdownMetaDao: CountsBreakdownMetaCacheDao,
    private val shiftingDestinationsDao: CountsShiftingDestinationsCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : CountsRepository {

    override fun observeHerdSummary(
        lifecycleStatus: String?,
        parkId: String?,
        breed: String?,
        sex: String?,
    ): Flow<Resource<HerdRegisterSummaryResponseDto>> {
        val key = summaryKey(lifecycleStatus, parkId, breed, sex)
        return summaryDao.observe(key)
            .map { entity ->
                val cached = readCachedJson<HerdRegisterSummaryResponseDto>(
                    json = json,
                    cacheKey = key,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { summaryDao.delete(it) },
                )
                Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
            }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshHerdSummary(
        lifecycleStatus: String?,
        parkId: String?,
        breed: String?,
        sex: String?,
    ): Result<Unit> = runCatching {
        val dto = api.getHerdRegisterSummary(lifecycleStatus, parkId, breed, sex)
        val key = summaryKey(lifecycleStatus, parkId, breed, sex)
        summaryDao.upsert(
            HerdSummaryCacheEntity(
                cacheKey = key,
                dtoJson = json.encodeToString(dto),
                updatedAt = clock(),
            ),
        )
        summaryDao.enforceCacheBounds()
    }

    override fun observeBreakdownTotals(
        query: CountsBreakdownQuery,
    ): Flow<Resource<CountsBreakdownResponseDto>> {
        val key = query.roomKey()
        return breakdownMetaDao.observe(key)
            .map { entity ->
                val cached = readCachedJson<CountsBreakdownResponseDto>(
                    json = json,
                    cacheKey = key,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { breakdownMetaDao.delete(it) },
                )
                Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
            }
            .flowOn(Dispatchers.Default)
    }

    @OptIn(ExperimentalPagingApi::class)
    override fun breakdownRows(query: CountsBreakdownQuery): Flow<PagingData<CountsBreakdownRowDto>> {
        val key = query.roomKey()
        return Pager(
            config = PagingConfig(
                pageSize = COUNTS_BREAKDOWN_PAGE_SIZE,
                initialLoadSize = COUNTS_BREAKDOWN_PAGE_SIZE,
                // Prefetch the next page as the user reaches ~item 17-18 of the current one.
                prefetchDistance = 3,
                enablePlaceholders = false,
                // Hard ceiling on rows retained in memory: three pages, then the far side is
                // dropped and re-read from Room on scroll back.
                maxSize = COUNTS_BREAKDOWN_PAGE_SIZE * 3,
            ),
            remoteMediator = CountsBreakdownRemoteMediator(
                query = query,
                api = api,
                database = database,
                metaDao = breakdownMetaDao,
                json = json,
                clock = clock,
            ),
            pagingSourceFactory = { database.countsBreakdownItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<CountsBreakdownRowDto>(entity.dtoJson) } }
            .flowOn(Dispatchers.Default)
    }

    override fun observeShiftingDestinations(): Flow<Resource<CountsShiftingDestinationsResponseDto>> =
        shiftingDestinationsDao.observe(SHIFTING_DESTINATIONS_CACHE_KEY)
            .map { entity ->
                val cached = readCachedJson<CountsShiftingDestinationsResponseDto>(
                    json = json,
                    cacheKey = SHIFTING_DESTINATIONS_CACHE_KEY,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { shiftingDestinationsDao.delete(it) },
                )
                Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
            }
            // Decode off Main: the catalog parses once, here, never on the UI thread.
            .flowOn(Dispatchers.Default)

    override suspend fun refreshShiftingDestinations(): Result<Unit> = runCatching {
        val dto = api.getCountsShiftingDestinations()
        shiftingDestinationsDao.upsert(
            CountsShiftingDestinationsCacheEntity(
                cacheKey = SHIFTING_DESTINATIONS_CACHE_KEY,
                dtoJson = json.encodeToString(dto),
                updatedAt = clock(),
            ),
        )
        shiftingDestinationsDao.enforceCacheBounds()
    }

    // A stale cached match would name an animal that has since exited or moved, and caching it
    // properly would mean an unbounded on-device whole-herd search index. See lookupAnimals' KDoc.
    override suspend fun lookupAnimals( // offline-first-guard:ignore: live tag->goat_id resolution, not a screen read model
        query: String,
        parkId: String?,
        shedId: String?,
    ): Result<List<GoatSearchItemDto>> = runCatching {
        val trimmed = query.trim()
        if (trimmed.isEmpty()) return@runCatching emptyList()
        api.searchGoats(
            q = trimmed,
            parkId = parkId?.takeIf { it.isNotBlank() },
            locationId = shedId?.takeIf { it.isNotBlank() },
            // Deliberately NO lifecycle-status filter. Which animals may be moved is a backend
            // rule, and a client-invented filter here gets it wrong in both directions:
            //   - the goats vocabulary has no "active" state at all (it is
            //     alive/sick/under_treatment/quarantine/icu/dead/sold/... ), so an invented value
            //     silently matches nothing and the picker finds no animal ever;
            //   - filtering to "alive" would hide precisely the sick / under_treatment /
            //     quarantine / icu animals that the `medical` and `quarantine` shifting
            //     CATEGORIES exist to move, making those movements impossible to record.
            // The endpoint is already scope-filtered to what this caller may see, and the
            // approval path is what authorizes the relocation.
            status = null,
            limit = COUNTS_ANIMAL_LOOKUP_PAGE_SIZE,
            cursor = null,
        ).items
    }

    private fun summaryKey(lifecycleStatus: String?, parkId: String?, breed: String?, sex: String?): String =
        cacheKey(lifecycleStatus, parkId, breed, sex)
}

/**
 * Fills Room from the backend page-by-page. Room stays the single source of truth: this never
 * hands rows to the UI, it only writes them, and the [androidx.paging.PagingSource] re-emits.
 *
 * `/counts/breakdown` paginates by `offset` over an AGGREGATED grain set (not by keyset), because
 * that is the shape the backend contract offers. The offset is persisted per scope in
 * `counts_breakdown_remote_keys`, so an APPEND after process death resumes at the right page
 * instead of re-walking from zero, and it always advances by the number of rows actually
 * returned — a short page ends pagination rather than looping on the same offset.
 */
@OptIn(ExperimentalPagingApi::class)
private class CountsBreakdownRemoteMediator(
    private val query: CountsBreakdownQuery,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val metaDao: CountsBreakdownMetaCacheDao,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, CountsBreakdownItemEntity>() {
    private val queryKey = query.roomKey()

    /** Cached inside the TTL: render Room immediately and skip the initial network round trip. */
    override suspend fun initialize(): InitializeAction {
        val cachedAt = database.countsBreakdownRemoteKeyDao().get(queryKey)?.updatedAt
        return if (cachedAt != null && clock() - cachedAt < CacheGovernance.DEFAULT_TTL_MILLIS) {
            InitializeAction.SKIP_INITIAL_REFRESH
        } else {
            InitializeAction.LAUNCH_INITIAL_REFRESH
        }
    }

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, CountsBreakdownItemEntity>,
    ): MediatorResult {
        val offset = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> 0
            LoadType.APPEND -> {
                val remoteKey = database.countsBreakdownRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextOffset
            }
        }
        return try {
            val response = api.getCountsBreakdown(
                parkId = query.parkId,
                shedId = query.shedId,
                managementStage = query.managementStage,
                breed = query.breed,
                sex = query.sex,
                lifecycleStatus = query.lifecycleStatus,
                limit = COUNTS_BREAKDOWN_PAGE_SIZE,
                offset = offset,
            )
            // A short page means the backend has nothing further for this scope. Checked against
            // the requested page size (not against total_rows, which counts the whole filtered
            // set) so pagination always terminates.
            val endReached = response.items.size < COUNTS_BREAKDOWN_PAGE_SIZE
            val updatedAt = clock()
            database.withTransaction {
                val itemDao = database.countsBreakdownItemDao()
                val remoteKeyDao = database.countsBreakdownRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                itemDao.upsertAll(
                    response.items.mapIndexed { index, row ->
                        CountsBreakdownItemEntity(
                            queryKey = queryKey,
                            grainKey = row.grainKey,
                            // Server order is preserved by offsetting the page's own index, so a
                            // later page never sorts above an earlier one.
                            sortIndex = offset + index,
                            dtoJson = json.encodeToString(row),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    CountsBreakdownRemoteKeyEntity(
                        queryKey = queryKey,
                        nextOffset = offset + response.items.size,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    metaDao.upsert(
                        CountsBreakdownMetaCacheEntity(
                            cacheKey = queryKey,
                            // Envelope only: the totals/charts/facets are fixed-size and are the
                            // authoritative KPIs. Rows are dropped here — they live as normalized
                            // Room rows so this blob can never grow with the cohort.
                            dtoJson = json.encodeToString(response.copy(items = emptyList())),
                            updatedAt = updatedAt,
                        ),
                    )
                    itemDao.deleteRowsOutsideNewestQueries(COUNTS_BREAKDOWN_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(COUNTS_BREAKDOWN_CACHED_QUERIES)
                }
            }
            if (loadType == LoadType.REFRESH) metaDao.enforceCacheBounds()
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
