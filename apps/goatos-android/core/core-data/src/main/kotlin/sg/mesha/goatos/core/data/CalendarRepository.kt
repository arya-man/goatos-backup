package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto

/**
 * Calendar screen area: the PC vaccination calendar (backend presentation +
 * bounded events). Thin pass-through over [AppApi]; DTO -> UiState mapping in :app.
 */
interface CalendarRepository {
    suspend fun events(
        parkId: String? = null,
        shedId: String? = null,
        ownerKey: String? = null,
        status: String? = null,
        dateFrom: String? = null,
        dateTo: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): CalendarEventListResponseDto
}

class DefaultCalendarRepository(
    private val api: AppApi,
) : CalendarRepository {
    override suspend fun events(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        cursor: String?,
        limit: Int?,
    ): CalendarEventListResponseDto =
        api.listCalendarVaccinationEvents(parkId, shedId, ownerKey, status, dateFrom, dateTo, cursor, limit)
}
