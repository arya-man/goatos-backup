package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * GET /app/vaccination/gaps -> VaccinationGapsResponse.
 * Animals excluded from vaccination coverage, one row PER ANIMAL, with the reason it's excluded
 * (missing DOB, missing breed, etc.). Backs the mobile "Data gaps" overlay. There is no by-reason
 * aggregate — every entry is a single goat.
 */
@Serializable
data class VaccinationGapRowDto(
    @SerialName("goatId") val goatId: String = "",
    @SerialName("displayId") val displayId: String = "",
    // Physical tags ("Tag 1" / "Tag 2") from goat_identifiers; null/absent when no active tag of
    // that type is attached — the card shows "—" in that slot.
    @SerialName("animalIdentifier1") val animalIdentifier1: String? = null,
    @SerialName("animalIdentifier2") val animalIdentifier2: String? = null,
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
    @SerialName("rows") val rows: List<VaccinationGapRowDto> = emptyList(),
    @SerialName("nextCursor") val nextCursor: String? = null,
)
