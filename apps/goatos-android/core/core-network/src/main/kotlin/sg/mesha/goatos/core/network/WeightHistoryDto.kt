package sg.mesha.goatos.core.network

import kotlinx.serialization.Serializable

@Serializable
data class WeightHistoryResponseDto(
    val parks: List<ParkDto> = emptyList(),
    val sheds: List<ShedDto> = emptyList(),
    val series: List<WeightSeriesDto> = emptyList(),
    val truncated: Boolean = false,
    val capped_at: String? = null,
)

@Serializable
data class ParkDto(
    val park_id: String,
    val name: String,
)

@Serializable
data class ShedDto(
    val campaign_shed_id: String,
    val display_name: String,
    val park_id: String,
    val location_id: String,
)

@Serializable
data class WeightSeriesDto(
    // NULL for a lump-sum series: the shed was weighed as a group, so no animal was scanned.
    // Declared non-null this failed the WHOLE payload to deserialize the moment one lump-sum
    // weighing existed, and the screen reported "couldn't fetch" for every animal too.
    val scanned_identifier: String? = null,
    val campaign_shed_id: String,
    val shed_display_name: String,
    val capture_kind: String,  // "individual" | "lump_sum"
    val points: List<WeightPointDto> = emptyList(),
)

@Serializable
data class WeightPointDto(
    val capture_kind: String,
    val scanned_identifier: String? = null,
    val weigh_date: String,  // YYYY-MM-DD
    val weight_kg: Double? = null,  // for individual
    val total_weight_kg: Double? = null,  // for lump_sum
    val average_weight_kg: Double? = null,  // for lump_sum
    val animal_count: Int? = null,  // for lump_sum
    val verification_status: String? = null,
    val campaign_shed_id: String,
    val shed_display_name: String,
)
