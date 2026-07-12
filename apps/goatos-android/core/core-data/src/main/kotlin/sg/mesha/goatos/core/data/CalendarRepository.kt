package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.CalendarCacheDao
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
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
}

class DefaultCalendarRepository(
    private val api: AppApi,
    private val dao: CalendarCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : CalendarRepository {
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
    ): Flow<Resource<CalendarEventListResponseDto>> =
        dao.observe(cacheKey(parkId, shedId, ownerKey, status, dateFrom, dateTo, includeDateMarkers.toString(), cursor, limit?.toString()))
            .map { it.toResource() }
            .flowOn(Dispatchers.Default)

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
    }

    private fun CalendarCacheEntity?.toResource(): Resource<CalendarEventListResponseDto> =
        Resource(
            data = this?.let { runCatching { json.decodeFromString<CalendarEventListResponseDto>(it.dtoJson) }.getOrNull() },
            lastSyncedAt = this?.updatedAt,
        )
}
