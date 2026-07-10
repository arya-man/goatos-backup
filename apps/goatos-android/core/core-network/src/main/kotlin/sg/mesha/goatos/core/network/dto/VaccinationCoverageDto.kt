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
    @SerialName("protocolId") val protocolId: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("givenCount") val givenCount: Int = 0,
    @SerialName("totalCount") val totalCount: Int = 0,
    @SerialName("coveragePercent") val coveragePercent: Int = 0,
)

@Serializable
data class VaccinationCoverageResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("parkId") val parkId: String? = null,
    @SerialName("protocols") val protocols: List<VaccinationCoverageProtocolDto> = emptyList(),
)
