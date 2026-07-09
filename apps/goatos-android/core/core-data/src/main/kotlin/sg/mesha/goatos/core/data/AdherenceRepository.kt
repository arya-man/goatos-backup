package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ProtocolAdherenceResponseDto

/**
 * Protocol Adherence screen area: expected-vs-actual vaccination adherence
 * (summary + rows). Thin pass-through over [AppApi]; DTO -> UiState mapping in :app.
 */
interface AdherenceRepository {
    suspend fun adherence(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): ProtocolAdherenceResponseDto
}

class DefaultAdherenceRepository(
    private val api: AppApi,
) : AdherenceRepository {
    override suspend fun adherence(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ProtocolAdherenceResponseDto =
        api.getVaccinationAdherence(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit)
}
