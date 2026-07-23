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
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.CalendarCacheDao
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.CalendarScheduleEntity
import sg.mesha.goatos.core.data.cache.CalendarScheduleRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.CacheGovernance
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate

const val CALENDAR_SCHEDULE_PAGE_SIZE = 20
private const val CALENDAR_SCHEDULE_CACHED_QUERIES = 24

/** One backend-filtered monthly schedule. The server still applies effective RBAC scope. */
data class CalendarScheduleQuery(
    val parkId: String? = null,
    val shedId: String? = null,
    val vaccine: String? = null,
    val status: String? = null,
    val ownerKey: String? = null,
    val dateFrom: String,
    val dateTo: String,
) {
    internal fun roomKey(): String = cacheKey(
        "calendar-schedule-v1",
        parkId,
        shedId,
        vaccine,
        status,
        ownerKey,
        dateFrom,
        dateTo,
        CALENDAR_SCHEDULE_PAGE_SIZE.toString(),
    )
}

/**
 * Calendar screen area: the PC vaccination calendar (backend presentation +
 * bounded events). Offline-first (docs/decisions/android-offline-first.md): Room is the
 * UI's single source of truth. [observeEvents] is cache-first and reactive; [refreshEvents]
 * is the network side of stale-while-revalidate — it upserts Room on success (which
 * re-emits to every observer) and leaves the cache untouched on failure. [events] is kept as
 * the plain network call [refreshEvents] wraps. DTO -> UiState mapping stays in :app.
 */
interface CalendarRepository {
    suspend fun events(
        parkId: String? = null,
        shedId: String? = null,
        ownerKey: String? = null,
        status: String? = null,
        dateFrom: String? = null,
        dateTo: String? = null,
        includeDateMarkers: Boolean = false,
        vaccine: String? = null,
        includeFilterOptions: Boolean = false,
        cursor: String? = null,
        limit: Int? = null,
    ): CalendarEventListResponseDto

    /** Cache-first stream for this filter scope: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshEvents]. */
    fun observeEvents(
        parkId: String? = null,
        shedId: String? = null,
        ownerKey: String? = null,
        status: String? = null,
        dateFrom: String? = null,
        dateTo: String? = null,
        includeDateMarkers: Boolean = false,
        vaccine: String? = null,
        includeFilterOptions: Boolean = false,
        cursor: String? = null,
        limit: Int? = null,
    ): Flow<Resource<CalendarEventListResponseDto>>

    /** Fetches and upserts Room on success; on failure returns the failure and leaves the
     *  cache untouched — the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshEvents(
        parkId: String? = null,
        shedId: String? = null,
        ownerKey: String? = null,
        status: String? = null,
        dateFrom: String? = null,
        dateTo: String? = null,
        includeDateMarkers: Boolean = false,
        vaccine: String? = null,
        includeFilterOptions: Boolean = false,
        cursor: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /**
     * MOB-004: Appends exactly the next server continuation page INTO the same Room-backed
     * first-page scope (the `cursor = null` scope key that [observeEvents] reads). This is the
     * on-device SSOT half of pagination: page 2+ is persisted in Room — never accumulated in
     * ViewModel memory — so it survives process death and is available offline. The observed
     * [observeEvents] flow re-emits the merged, bounded keyset window; the UI never renders a
     * direct-network DTO. Mirrors [ExecutionRepository.appendRows].
     */
    suspend fun appendEvents(
        cursor: String,
        parkId: String? = null,
        shedId: String? = null,
        ownerKey: String? = null,
        status: String? = null,
        dateFrom: String? = null,
        dateTo: String? = null,
        includeDateMarkers: Boolean = false,
        vaccine: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /** Room PagingSource + backend keyset RemoteMediator; collection triggers the first read. */
    fun schedule(query: CalendarScheduleQuery): Flow<PagingData<CalendarEventDto>>

    /** Presentation/filter metadata cached alongside the first page for stale-while-revalidate. */
    fun observeScheduleMetadata(query: CalendarScheduleQuery): Flow<Resource<CalendarEventListResponseDto>>
}

class DefaultCalendarRepository(
    private val api: AppApi,
    private val dao: CalendarCacheDao,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : CalendarRepository {
    private val eventsAppendMutex = Mutex()

    override suspend fun events(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        includeFilterOptions: Boolean,
        cursor: String?,
        limit: Int?,
    ): CalendarEventListResponseDto =
        api.listCalendarVaccinationEvents(
            parkId = parkId,
            shedId = shedId,
            ownerKey = ownerKey,
            status = status,
            dateFrom = dateFrom,
            dateTo = dateTo,
            includeDateMarkers = includeDateMarkers,
            vaccine = vaccine,
            includeFilterOptions = includeFilterOptions,
            cursor = cursor,
            limit = limit,
        )

    override fun observeEvents(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        includeFilterOptions: Boolean,
        cursor: String?,
        limit: Int?,
    ): Flow<Resource<CalendarEventListResponseDto>> {
        val key = eventCacheKey(
            parkId,
            shedId,
            ownerKey,
            status,
            dateFrom,
            dateTo,
            includeDateMarkers,
            vaccine,
            includeFilterOptions,
            cursor,
            limit,
        )
        return dao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refreshEvents(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        includeFilterOptions: Boolean,
        cursor: String?,
        limit: Int?,
    ): Result<Unit> = try {
        val dto = events(
            parkId,
            shedId,
            ownerKey,
            status,
            dateFrom,
            dateTo,
            includeDateMarkers,
            vaccine,
            includeFilterOptions,
            cursor,
            limit,
        )
        val key = eventCacheKey(
            parkId,
            shedId,
            ownerKey,
            status,
            dateFrom,
            dateTo,
            includeDateMarkers,
            vaccine,
            includeFilterOptions,
            cursor,
            limit,
        )
        dao.upsert(CalendarCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        dao.enforceCacheBounds()
        Result.success(Unit)
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (error: Exception) {
        Result.failure(error)
    }

    override suspend fun appendEvents(
        cursor: String,
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        limit: Int?,
    ): Result<Unit> = try {
        eventsAppendMutex.withLock {
            // The scope key the UI observes is the first-page key (cursor = null); every page
            // merges into THAT row so the observed Room flow re-emits the growing keyset window.
            val key = eventCacheKey(
                parkId,
                shedId,
                ownerKey,
                status,
                dateFrom,
                dateTo,
                includeDateMarkers,
                vaccine,
                false,
                null,
                limit,
            )
            val currentEntity = dao.get(key)
            val current = readCachedJson<CalendarEventListResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = currentEntity?.dtoJson,
                updatedAt = currentEntity?.updatedAt,
                now = clock(),
                quarantine = { dao.delete(it) },
            ).data ?: throw CalendarEventsCursorException("calendar continuation has no cached first page")
            if (current.nextCursor != cursor) {
                throw CalendarEventsCursorException("calendar cursor is stale or belongs to another filter")
            }
            val page = events(
                parkId,
                shedId,
                ownerKey,
                status,
                dateFrom,
                dateTo,
                includeDateMarkers,
                vaccine,
                false,
                cursor,
                limit,
            )
            if (page.nextCursor == cursor) {
                throw CalendarEventsCursorException("calendar backend returned a non-advancing cursor")
            }
            dao.upsert(
                CalendarCacheEntity(
                    cacheKey = key,
                    dtoJson = json.encodeToString(mergeCalendarEventsPage(current, page)),
                    updatedAt = clock(),
                ),
            )
        }
        Result.success(Unit)
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (error: Exception) {
        Result.failure(error)
    }

    @OptIn(ExperimentalPagingApi::class)
    override fun schedule(query: CalendarScheduleQuery): Flow<PagingData<CalendarEventDto>> {
        val key = query.roomKey()
        val scheduleDao = database.calendarScheduleDao()
        return Pager(
            config = PagingConfig(
                pageSize = CALENDAR_SCHEDULE_PAGE_SIZE,
                initialLoadSize = CALENDAR_SCHEDULE_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = CALENDAR_SCHEDULE_PAGE_SIZE * 3,
            ),
            remoteMediator = CalendarScheduleRemoteMediator(
                query = query,
                api = api,
                database = database,
                metadataDao = dao,
                json = json,
                clock = clock,
            ),
            pagingSourceFactory = {
                scheduleDao.pagingSource(queryKey = key)
            },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<CalendarEventDto>(entity.dtoJson) } }
            .flowOn(Dispatchers.Default)
    }

    override fun observeScheduleMetadata(query: CalendarScheduleQuery): Flow<Resource<CalendarEventListResponseDto>> {
        val key = query.roomKey()
        return dao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default)
    }

    private suspend fun CalendarCacheEntity?.toResource(key: String): Resource<CalendarEventListResponseDto> {
        val cached = readCachedJson<CalendarEventListResponseDto>(
            json = json,
            cacheKey = key,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { dao.delete(it) },
        )
        return Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
    }
}

private fun eventCacheKey(
    parkId: String?,
    shedId: String?,
    ownerKey: String?,
    status: String?,
    dateFrom: String?,
    dateTo: String?,
    includeDateMarkers: Boolean,
    vaccine: String?,
    includeFilterOptions: Boolean,
    cursor: String?,
    limit: Int?,
): String {
    // Keep existing cache keys stable for every pre-schedule caller.
    if (vaccine == null && !includeFilterOptions) {
        return cacheKey(
            parkId,
            shedId,
            ownerKey,
            status,
            dateFrom,
            dateTo,
            includeDateMarkers.toString(),
            cursor,
            limit?.toString(),
        )
    }
    return cacheKey(
        parkId,
        shedId,
        ownerKey,
        status,
        dateFrom,
        dateTo,
        includeDateMarkers.toString(),
        vaccine,
        includeFilterOptions.toString(),
        cursor,
        limit?.toString(),
    )
}

@OptIn(ExperimentalPagingApi::class)
private class CalendarScheduleRemoteMediator(
    private val query: CalendarScheduleQuery,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val metadataDao: CalendarCacheDao,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, CalendarScheduleEntity>() {
    private val queryKey = query.roomKey()

    override suspend fun initialize(): InitializeAction {
        val cachedAt = database.calendarScheduleRemoteKeyDao().get(queryKey)?.updatedAt
        return if (cachedAt != null && clock() - cachedAt < CacheGovernance.DEFAULT_TTL_MILLIS) {
            InitializeAction.SKIP_INITIAL_REFRESH
        } else {
            InitializeAction.LAUNCH_INITIAL_REFRESH
        }
    }

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, CalendarScheduleEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.calendarScheduleRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                remoteKey.nextCursor
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
            }
        }
        return try {
            val response = api.listCalendarVaccinationEvents(
                parkId = query.parkId,
                shedId = query.shedId,
                ownerKey = query.ownerKey,
                status = query.status,
                dateFrom = query.dateFrom,
                dateTo = query.dateTo,
                includeDateMarkers = false,
                vaccine = query.vaccine,
                includeFilterOptions = loadType == LoadType.REFRESH,
                cursor = cursor,
                limit = CALENDAR_SCHEDULE_PAGE_SIZE,
            )
            val updatedAt = clock()
            database.withTransaction {
                val scheduleDao = database.calendarScheduleDao()
                val remoteKeyDao = database.calendarScheduleRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    scheduleDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                }
                scheduleDao.upsertAll(
                    response.items.map { item ->
                        CalendarScheduleEntity(
                            queryKey = queryKey,
                            eventId = item.eventId,
                            dueAt = item.currentScheduleDate,
                            dtoJson = json.encodeToString(item),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    CalendarScheduleRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = response.nextCursor,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    metadataDao.upsert(
                        CalendarCacheEntity(
                            cacheKey = queryKey,
                            // Metadata is fixed-size; normalized Room rows own the page data.
                            dtoJson = json.encodeToString(
                                response.copy(items = emptyList(), dateMarkers = emptyList(), nextCursor = null),
                            ),
                            updatedAt = updatedAt,
                        ),
                    )
                    scheduleDao.deleteRowsOutsideNewestQueries(CALENDAR_SCHEDULE_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(CALENDAR_SCHEDULE_CACHED_QUERIES)
                }
            }
            if (loadType == LoadType.REFRESH) metadataDao.enforceCacheBounds()
            MediatorResult.Success(endOfPaginationReached = response.nextCursor == null)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            MediatorResult.Error(error)
        }
    }
}

class CalendarEventsCursorException(message: String) : IllegalStateException(message)

/**
 * MOB-004: Merges the next continuation page onto the cached first page. Items extend the
 * bounded keyset window (de-duplicated by `event_id`, preserving order: page 1 rows first,
 * then page 2+ rows) and the cursor advances. The authoritative first-page `presentation`,
 * `source`, and `date_markers` are preserved from `current` — continuation pages carry list
 * rows only.
 */
internal fun mergeCalendarEventsPage(
    current: CalendarEventListResponseDto,
    page: CalendarEventListResponseDto,
): CalendarEventListResponseDto {
    val merged = (current.items + page.items).distinctBy { it.eventId }
    val capped = merged.take(CacheGovernance.DEFAULT_MAX_ROWS) // mobile-guard:ignore: explicit 200-row cache-governance ceiling across user-requested pages
    return current.copy(
        items = capped,
        nextCursor = page.nextCursor?.takeIf { merged.size <= CacheGovernance.DEFAULT_MAX_ROWS },
    )
}
