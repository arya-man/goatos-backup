package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto

/**
 * Vaccination execution screen area: the execution row list and the per-shed
 * drilldown. Thin pass-through over [AppApi] — DTO -> UiState mapping stays in :app.
 */
interface ExecutionRepository {
    suspend fun rows(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): VaccinationExecutionResponseDto

    suspend fun shed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): VaccinationExecutionShedDrilldownDto

    /** Per-animal scan roster (RFID tags + due vaccine) for a shed. */
    suspend fun scanRoster(
        shedId: String,
        limit: Int? = null,
    ): ScanRosterResponseDto
}

class DefaultExecutionRepository(
    private val api: AppApi,
) : ExecutionRepository {
    override suspend fun rows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationExecutionResponseDto =
        api.listVaccinationExecution(parkId, workState, asOf, dueBefore, limit)

    override suspend fun shed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationExecutionShedDrilldownDto =
        api.getVaccinationExecutionShed(shedId, asOf, dueBefore, limit)

    override suspend fun scanRoster(
        shedId: String,
        limit: Int?,
    ): ScanRosterResponseDto = api.getScanRoster(shedId, limit)
}
