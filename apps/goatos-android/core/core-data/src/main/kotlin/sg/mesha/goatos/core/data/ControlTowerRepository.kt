package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto

/**
 * Control Tower screen area: broken/at-risk vaccination process integrity
 * (summary + alerts). Thin pass-through over [AppApi]; DTO -> UiState mapping in :app.
 */
interface ControlTowerRepository {
    suspend fun summary(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): ControlTowerResponseDto
}

class DefaultControlTowerRepository(
    private val api: AppApi,
) : ControlTowerRepository {
    override suspend fun summary(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ControlTowerResponseDto =
        api.getVaccinationControlTower(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit)
}
