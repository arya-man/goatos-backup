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
    @SerialName("reason") val reason: String = "",
    @SerialName("count") val count: Int = 0,
)

@Serializable
data class VaccinationGapRowDto(
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("rfid") val rfid: String? = null,
    @SerialName("old_tag") val oldTag: String? = null,
    @SerialName("reason") val reason: String = "",
)

@Serializable
data class VaccinationGapsResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("reasons") val reasons: List<VaccinationGapReasonSummaryDto> = emptyList(),
    @SerialName("rows") val rows: List<VaccinationGapRowDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)
