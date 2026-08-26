package sg.mesha.goatos.core.network

import kotlinx.serialization.Serializable

/**
 * Leadership growth (ADG) summary, computed from WEIGHING TABLES ONLY.
 *
 * Every ADG figure is NULLABLE on purpose. A park whose animals have each been weighed once has
 * no computable growth, and "unknown" must never arrive as 0.0 -- a zero would read as "the herd
 * is not growing", which is a different and much worse claim than "we cannot know yet".
 */
@Serializable
data class GrowthSummaryDto(
    val park_id: String? = null,
    /** Every park aggregated into this answer. Present for the all-parks view. */
    val park_ids: List<String> = emptyList(),
    /** Real park names for the scope selector, so a client never invents "Park 1". */
    val parks: List<GrowthParkDto> = emptyList(),
    val period_start: String = "",
    val period_end: String = "",
    val headline: GrowthHeadlineDto = GrowthHeadlineDto(),
    val eligibility: GrowthEligibilityDto = GrowthEligibilityDto(),
    val trend: List<GrowthTrendPointDto> = emptyList(),
    val shed_leaderboard: List<GrowthShedDto> = emptyList(),
    /**
     * Group-weighed sheds. Kept SEPARATE from everything above on purpose: a lump-sum weighing is
     * one total and a head count, with no per-animal identity, so no ADG can be derived from it --
     * the population may differ between two weighs. Mixing it into the per-animal numbers would be
     * arithmetic across two different units.
     */
    val lump_sum: GrowthLumpSumDto = GrowthLumpSumDto(),
    val losing_animals: List<GrowthLosingAnimalDto> = emptyList(),
)

@Serializable
data class GrowthLosingAnimalDto(
    val scanned_identifier: String = "",
    val shed_display_name: String = "",
    val previous_weight_kg: Double = 0.0,
    val latest_weight_kg: Double = 0.0,
    val adg_g_per_day: Double = 0.0,
    val days_between: Double = 0.0,
    val latest_weigh_date: String = "",
)

@Serializable
data class GrowthLumpSumDto(
    val shed_week_trend: List<GrowthLumpSumPointDto> = emptyList(),
)

@Serializable
data class GrowthLumpSumPointDto(
    val location_id: String = "",
    val display_name: String = "",
    val week_start: String = "",
    val average_weight_kg: Double? = null,
    val head_count: Int = 0,
)

@Serializable
data class GrowthHeadlineDto(
    /** "ok" | "insufficient_data" — the explicit marker that separates unknown from zero. */
    val status: String = "",
    val previous_status: String = "",
    /**
     * The farm's daily gain: the animal-weighted mean over kids weighed twice PLUS whole-shed pens,
     * each pen counting once per animal it holds. Was the median of scanned pairs alone, which left
     * out the pens most of this farm's kids are weighed in and disagreed with the web gain charts.
     */
    val average_adg_g_per_day: Double? = null,
    val previous_average_adg_g_per_day: Double? = null,
    /** Absent when there is no comparable previous period -- never a delta derived from zero. */
    val delta_g_per_day: Double? = null,
    val positive_adg_percent: Double? = null,
    /** PAIRS that dipped across the period — a measurement statistic, not an animal count. */
    val negative_adg_count: Int = 0,
    /** ANIMALS losing weight on their latest weigh. This is what the tile shows and drills into. */
    val losing_animal_count: Int = 0,
    /** Scanned pairs: the denominator of the pair statistics above, NOT of the gain. */
    val pair_count: Int = 0,
    /** How many kids the gain speaks for — scanned pairs plus the animals in whole-shed pens. */
    val headline_animals: Int = 0,
    val rejected_observation_count: Int = 0,
    val unverified_observation_count: Int = 0,
)

@Serializable
data class GrowthEligibilityDto(
    /** Growth needs two weighs of the SAME scanned tag. This is the denominator of any claim. */
    val animals_with_two_plus_weighs: Int = 0,
    val total_animals_weighed: Int = 0,
)

@Serializable
data class GrowthTrendPointDto(
    val week_start: String = "",
    val median_adg_g_per_day: Double? = null,
    val pair_count: Int = 0,
)

@Serializable
data class GrowthShedDto(
    val location_id: String = "",
    val display_name: String = "",
    val n: Int = 0,
    val median_weight_kg: Double? = null,
    val median_adg_g_per_day: Double? = null,
    val adg_pair_count: Int = 0,
)

@Serializable
data class GrowthParkDto(
    val park_id: String = "",
    val name: String = "",
)
