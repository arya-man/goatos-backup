package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * GET /app/vaccination/coverage -> VaccinationCoverageResponse.
 * Per-vaccine given-dose count + coverage % rollup for a scope.
 * Backs the mobile "Doses given" overlay.
 */
@Serializable
data class VaccinationCoverageProtocolDto(
    @SerialName("protocol_id") val protocolId: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("given_count") val givenCount: Int = 0,
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("coverage_percent") val coveragePercent: Int = 0,
)

@Serializable
data class VaccinationCoverageResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("protocols") val protocols: List<VaccinationCoverageProtocolDto> = emptyList(),
)
