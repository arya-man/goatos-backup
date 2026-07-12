package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.CalendarCacheDao
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto

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
        cursor: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    /**
     * MOB-004: Appends exactly the next server continuation page INTO the same Room-backed
     * first-page scope (the `cursor = null` scope key that [observeEvents] reads). This is the
     * on-device SSOT half of pagination: page 2+ is persisted in Room — never accumulated in
     * ViewModel memory — so it survives process death and is available offline. The observed
     * [observeEvents] flow re-emits the merged, bounded keyset window; the UI never renders a
     * direct-network DTO. Mirrors [ExecutionRepository.appendRows] / [ExecutionRepository.appendScanRoster].
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
        limit: Int? = null,
    ): Result<Unit>
}

class DefaultCalendarRepository(
    private val api: AppApi,
    private val dao: CalendarCacheDao,
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
        cursor: String?,
        limit: Int?,
    ): CalendarEventListResponseDto =
        api.listCalendarVaccinationEvents(parkId, shedId, ownerKey, status, dateFrom, dateTo, includeDateMarkers, cursor, limit)

    override fun observeEvents(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        cursor: String?,
        limit: Int?,
    ): Flow<Resource<CalendarEventListResponseDto>> {
        val key = cacheKey(parkId, shedId, ownerKey, status, dateFrom, dateTo, includeDateMarkers.toString(), cursor, limit?.toString())
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
        cursor: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        val dto = events(parkId, shedId, ownerKey, status, dateFrom, dateTo, includeDateMarkers, cursor, limit)
        val key = cacheKey(parkId, shedId, ownerKey, status, dateFrom, dateTo, includeDateMarkers.toString(), cursor, limit?.toString())
        dao.upsert(CalendarCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        dao.enforceCacheBounds()
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
        limit: Int?,
    ): Result<Unit> = runCatching {
        eventsAppendMutex.withLock {
            // The scope key the UI observes is the first-page key (cursor = null); every page
            // merges into THAT row so the observed Room flow re-emits the growing keyset window.
            val key = cacheKey(parkId, shedId, ownerKey, status, dateFrom, dateTo, includeDateMarkers.toString(), null, limit?.toString())
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
            val page = events(parkId, shedId, ownerKey, status, dateFrom, dateTo, includeDateMarkers, cursor, limit)
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
): CalendarEventListResponseDto = current.copy(
    items = (current.items + page.items).distinctBy { it.eventId },
    nextCursor = page.nextCursor,
)
