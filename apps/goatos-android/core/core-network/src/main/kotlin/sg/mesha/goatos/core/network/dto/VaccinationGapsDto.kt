package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * GET /app/vaccination/gaps -> VaccinationGapsResponse.
 * Animals excluded from vaccination coverage with reasons (missing DOB, missing breed, etc.).
 * Backs the mobile "Data gaps" overlay.
 */
@Serializable
data class VaccinationGapReasonSummaryDto(
    @SerialName("reasonCode") val reasonCode: String = "",
    @SerialName("reasonLabel") val reasonLabel: String = "",
    @SerialName("count") val count: Int = 0,
)

@Serializable
data class VaccinationGapRowDto(
    @SerialName("goatId") val goatId: String = "",
    @SerialName("displayId") val displayId: String = "",
    @SerialName("parkId") val parkId: String = "",
    @SerialName("parkName") val parkName: String = "",
    @SerialName("shedId") val shedId: String? = null,
    @SerialName("shedName") val shedName: String? = null,
    @SerialName("reasonCode") val reasonCode: String = "",
    @SerialName("reasonLabel") val reasonLabel: String = "",
)

@Serializable
data class VaccinationGapsResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("parkId") val parkId: String? = null,
    @SerialName("reasons") val reasons: List<VaccinationGapReasonSummaryDto> = emptyList(),
    @SerialName("rows") val rows: List<VaccinationGapRowDto> = emptyList(),
    @SerialName("nextCursor") val nextCursor: String? = null,
)
