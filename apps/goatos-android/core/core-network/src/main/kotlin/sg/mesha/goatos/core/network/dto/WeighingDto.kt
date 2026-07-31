package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class WeighingProgressDto(
    @SerialName("individual_expected_count") val individualExpectedCount: Int = 0,
    @SerialName("individual_completed_count") val individualCompletedCount: Int = 0,
    @SerialName("per_scope_expected_count") val perScopeExpectedCount: Int = 0,
    @SerialName("per_scope_completed_count") val perScopeCompletedCount: Int = 0,
    @SerialName("wrong_shed_count") val wrongShedCount: Int = 0,
    @SerialName("missing_count") val missingCount: Int = 0,
    @SerialName("remaining_count") val remainingCount: Int = 0,
)

@Serializable
data class WeighingCampaignShedDto(
    @SerialName("campaign_shed_id") val campaignShedId: String = "",
    @SerialName("campaign_id") val campaignId: String = "",
    @SerialName("location_id") val locationId: String = "",
    @SerialName("location_type") val locationType: String = "",
    @SerialName("display_name") val displayName: String = "",
    @SerialName("expected_animal_count") val expectedAnimalCount: Int = 0,
    @SerialName("weighing_category") val weighingCategory: String = "",
    @SerialName("operator_user_id") val operatorUserId: String = "",
    @SerialName("status") val status: String = "",
    /** Backend-owned count of animals still open in THIS shed bucket. Absent when the
     *  backend read model does not publish shed-grain remaining truth yet; the client
     *  must then render a count-free close label instead of inventing a number. */
    @SerialName("remaining_count") val remainingCount: Int? = null,
)

@Serializable
data class WeighingCampaignDto(
    @SerialName("campaign_id") val campaignId: String = "",
    @SerialName("tenant_id") val tenantId: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_name") val parkName: String = "",
    @SerialName("period_start_date") val periodStartDate: String = "",
    @SerialName("period_end_date") val periodEndDate: String = "",
    @SerialName("start_business_date") val startBusinessDate: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("planned_cap_per_day") val plannedCapPerDay: Int = 0,
    @SerialName("operator_user_id") val operatorUserId: String = "",
    @SerialName("created_by") val createdBy: String = "",
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
    @SerialName("row_version") val rowVersion: Int = 0,
    @SerialName("sheds") val sheds: List<WeighingCampaignShedDto> = emptyList(),
    @SerialName("progress") val progress: WeighingProgressDto = WeighingProgressDto(),
)

@Serializable
data class WeighingCampaignListResponseDto(
    @SerialName("items") val items: List<WeighingCampaignDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("trace_id") val traceId: String? = null,
)

@Serializable
data class WeighingPlannerCatalogResponseDto(
    @SerialName("parks") val parks: List<WeighingPlannerParkDto> = emptyList(),
    @SerialName("operators") val operators: List<WeighingPlannerOperatorDto> = emptyList(),
    @SerialName("trace_id") val traceId: String? = null,
)

@Serializable
data class WeighingPlannerParkDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("kid_count") val kidCount: Int = 0,
    @SerialName("sheds") val sheds: List<WeighingPlannerShedDto> = emptyList(),
    @SerialName("existing_campaign") val existingCampaign: WeighingCampaignSummaryDto? = null,
)

@Serializable
data class WeighingPlannerShedDto(
    @SerialName("location_id") val locationId: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("kid_count") val kidCount: Int = 0,
)

@Serializable
data class WeighingCampaignSummaryDto(
    @SerialName("campaign_id") val campaignId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("period_start_date") val periodStartDate: String = "",
    @SerialName("period_end_date") val periodEndDate: String = "",
    @SerialName("start_business_date") val startBusinessDate: String = "",
    @SerialName("operator_user_id") val operatorUserId: String = "",
    @SerialName("shed_count") val shedCount: Int = 0,
)

@Serializable
data class WeighingPlannerOperatorDto(
    @SerialName("user_id") val userId: String = "",
    @SerialName("display_name") val displayName: String = "",
    @SerialName("display_code") val displayCode: String = "",
)

@Serializable
data class WeighingCreateCampaignRequestDto(
    @SerialName("park_id") val parkId: String,
    @SerialName("period_start_date") val periodStartDate: String,
    @SerialName("period_end_date") val periodEndDate: String,
    @SerialName("start_business_date") val startBusinessDate: String,
    @SerialName("planned_cap_per_day") val plannedCapPerDay: Int,
    @SerialName("operator_user_id") val operatorUserId: String,
    @SerialName("sheds") val sheds: List<WeighingCreateCampaignShedDto>,
)

@Serializable
data class WeighingCreateCampaignShedDto(
    @SerialName("location_id") val locationId: String,
    @SerialName("location_type") val locationType: String,
    @SerialName("display_name") val displayName: String,
    @SerialName("weighing_category") val weighingCategory: String,
    @SerialName("operator_user_id") val operatorUserId: String? = null,
)

@Serializable
data class WeighingCampaignResponseDto(
    @SerialName("campaign") val campaign: WeighingCampaignDto = WeighingCampaignDto(),
    @SerialName("trace_id") val traceId: String? = null,
)

@Serializable
data class WeighingRosterRowDto(
    @SerialName("campaign_id") val campaignId: String = "",
    @SerialName("campaign_shed_id") val campaignShedId: String = "",
    @SerialName("animal_id") val animalId: String = "",
    @SerialName("display_animal_id") val displayAnimalId: String = "",
    @SerialName("primary_identifier") val primaryIdentifier: String = "",
    @SerialName("secondary_identifier") val secondaryIdentifier: String? = null,
    @SerialName("expected_location_id") val expectedLocationId: String = "",
    @SerialName("expected_location_label") val expectedLocationLabel: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("availability_status") val availabilityStatus: String? = null,
    @SerialName("current_location_id") val currentLocationId: String? = null,
    @SerialName("current_location_label") val currentLocationLabel: String? = null,
    @SerialName("current_lifecycle_status") val currentLifecycleStatus: String? = null,
    @SerialName("seq") val seq: Long = 0,
)

@Serializable
data class WeighingRosterResponseDto(
    @SerialName("items") val items: List<WeighingRosterRowDto> = emptyList(),
    @SerialName("observations") val observations: List<WeighingAcceptedObservationDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    // Independently paginates `observations` on a keyset of (accepted_at,
    // observation_id) scoped to this campaign_shed_id; absent/null means no
    // further observations page for this request.
    @SerialName("next_observations_cursor") val nextObservationsCursor: String? = null,
    @SerialName("trace_id") val traceId: String? = null,
)

@Serializable
data class WeighingAcceptedObservationDto(
    @SerialName("observation_id") val observationId: String = "",
    @SerialName("campaign_id") val campaignId: String = "",
    @SerialName("campaign_shed_id") val campaignShedId: String = "",
    @SerialName("animal_id") val animalId: String = "",
    @SerialName("scanned_identifier") val scannedIdentifier: String = "",
    @SerialName("weight_kg") val weightKg: Double = 0.0,
    @SerialName("average_weight_kg") val averageWeightKg: Double = 0.0,
    @SerialName("animal_count") val animalCount: Int = 0,
    @SerialName("proof_artifact_id") val proofArtifactId: String = "",
    @SerialName("proof_artifact_ids") val proofArtifactIds: List<String> = emptyList(),
    @SerialName("expected_location_id") val expectedLocationId: String = "",
    @SerialName("accepted_at") val acceptedAt: String = "",
)

@Serializable
data class WeighingAnimalObservationRequestDto(
    @SerialName("campaign_shed_id") val campaignShedId: String,
    @SerialName("scanned_identifier") val scannedIdentifier: String,
    @SerialName("weight_kg") val weightKg: Double,
    @SerialName("proof_artifact_id") val proofArtifactId: String,
    @SerialName("actual_location_id") val actualLocationId: String,
)

@Serializable
data class WeighingShedObservationRequestDto(
    @SerialName("campaign_shed_id") val campaignShedId: String,
    @SerialName("weight_kg") val weightKg: Double,
    @SerialName("animal_count") val animalCount: Int = 1,
    @SerialName("average_weight_kg") val averageWeightKg: Double = weightKg,
    @SerialName("proof_artifact_id") val proofArtifactId: String,
    @SerialName("proof_artifact_ids") val proofArtifactIds: List<String> = emptyList(),
)

@Serializable
data class WeighingObservationDto(
    @SerialName("observation_id") val observationId: String = "",
    @SerialName("campaign_id") val campaignId: String = "",
    @SerialName("campaign_shed_id") val campaignShedId: String? = null,
    @SerialName("animal_id") val animalId: String? = null,
    @SerialName("weight_kg") val weightKg: Double = 0.0,
    @SerialName("average_weight_kg") val averageWeightKg: Double = 0.0,
    @SerialName("animal_count") val animalCount: Int = 0,
    @SerialName("proof_artifact_id") val proofArtifactId: String = "",
    @SerialName("proof_artifact_ids") val proofArtifactIds: List<String> = emptyList(),
    @SerialName("expected_location_id") val expectedLocationId: String? = null,
    @SerialName("actual_location_id") val actualLocationId: String? = null,
    @SerialName("actual_location_label") val actualLocationLabel: String? = null,
    @SerialName("accepted_at") val acceptedAt: String = "",
    @SerialName("media") val media: List<WeighingProofMediaDto> = emptyList(),
)

@Serializable
data class WeighingProofMediaDto(
    @SerialName("proof_id") val proofId: String = "",
    @SerialName("download_url") val downloadUrl: String = "",
    @SerialName("mime_type") val mimeType: String = "",
)

@Serializable
data class WeighingLeadershipShedVideosDto(
    @SerialName("campaign_id") val campaignId: String = "",
    @SerialName("campaign_shed_id") val campaignShedId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("weighing_category") val weighingCategory: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("individual") val individual: List<WeighingObservationDto> = emptyList(),
    @SerialName("lump_sum") val lumpSum: WeighingObservationDto? = null,
)

@Serializable
data class WeighingLeadershipShedVideosResponseDto(
    @SerialName("shed") val shed: WeighingLeadershipShedVideosDto = WeighingLeadershipShedVideosDto(),
    @SerialName("trace_id") val traceId: String? = null,
)

@Serializable
data class WeighingObservationResponseDto(
    @SerialName("observation") val observation: WeighingObservationDto = WeighingObservationDto(),
    @SerialName("trace_id") val traceId: String? = null,
)

@Serializable
data class WeighingScopeReopenRequestDto(
    @SerialName("reason") val reason: String = "",
)

@Serializable
data class WeighingScopeCloseRequestDto(
    @SerialName("reason") val reason: String = "",
    @SerialName("idempotency_key") val idempotencyKey: String = "",
)

@kotlinx.serialization.Serializable
data class WeighingScopeSubmitRequestDto(
    @kotlinx.serialization.SerialName("scanned_identifiers")
    val scannedIdentifiers: List<String>,
)
