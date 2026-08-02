package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class WeighingProgressDto(
    /** Backend-owned count; do NOT render as a denominator. Weighing is free-flow with no expected roster.
     *  This field is sent by the backend but MUST NOT be used to compute completeness ratios or missing counts. */
    @SerialName("individual_expected_count") val individualExpectedCount: Int = 0,
    @SerialName("individual_completed_count") val individualCompletedCount: Int = 0,
    /** Backend-owned count; do NOT render as a denominator. Weighing is free-flow with no expected roster. */
    @SerialName("per_scope_expected_count") val perScopeExpectedCount: Int = 0,
    @SerialName("per_scope_completed_count") val perScopeCompletedCount: Int = 0,
    @SerialName("wrong_shed_count") val wrongShedCount: Int = 0,
    /** Backend-owned count; do NOT render as "missing animals". Weighing has no expected roster tracking. */
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
    /** Backend-owned count; do NOT render as a denominator. Weighing is free-flow with no expected roster.
     *  This field is sent for historical reasons but MUST NOT be used to compute completeness ratios. */
    @SerialName("expected_animal_count") val expectedAnimalCount: Int = 0,
    @SerialName("weighing_category") val weighingCategory: String = "",
    @SerialName("operator_user_id") val operatorUserId: String = "",
    /**
     * Backend-resolved assignee name, carried ON the bucket. Blank WITH a non-blank
     * [operatorUserId] is a roster gap, NOT "not assigned yet". The client must never resolve this
     * by joining the bucket against a separately paged operator vocabulary.
     */
    @SerialName("operator_display_name") val operatorDisplayName: String = "",
    @SerialName("status") val status: String = "",
    /** Backend-owned count of animals still open in THIS shed bucket. Absent when the
     *  backend read model does not publish shed-grain remaining truth yet; the client
     *  must then render a count-free close label instead of inventing a number. */
    @SerialName("remaining_count") val remainingCount: Int? = null,
    /** Backend-owned count of this bucket's submitted evidence still awaiting a verifier look. */
    @SerialName("pending_verification_count") val pendingVerificationCount: Int = 0,
    /** Strict subset of [pendingVerificationCount] a verifier bounced back to the operator. */
    @SerialName("rework_count") val reworkCount: Int = 0,
    /** True only when this bucket is submitted and every observation on it is verified. */
    @SerialName("ready_to_close") val readyToClose: Boolean = false,
)

/**
 * One keyset page of ONE task's shed buckets (`GET /app/weighing/campaigns/{id}/sheds`).
 *
 * [totalCount] ranges over the WHOLE task, not this page, so a header does not change while the
 * reader scrolls.
 */
@Serializable
data class WeighingCampaignShedPageResponseDto(
    @SerialName("campaign_id") val campaignId: String = "",
    @SerialName("items") val items: List<WeighingCampaignShedDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("trace_id") val traceId: String? = null,
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
    /** Backend-recorded reason the task was ended. Blank while the task is live. */
    @SerialName("close_reason") val closeReason: String = "",
    @SerialName("sheds") val sheds: List<WeighingCampaignShedDto> = emptyList(),
    @SerialName("progress") val progress: WeighingProgressDto = WeighingProgressDto(),
)

@Serializable
data class WeighingCampaignCountsDto(
    @SerialName("active") val active: Int = 0,
    @SerialName("completed") val completed: Int = 0,
)

/**
 * Which task-level writes THIS caller may attempt.
 *
 * Publish and end are held by DIFFERENT permissions, so a client that gates a button on task
 * status alone renders a live action that 403s for a real role. Defaults are false: an older
 * server that does not send this offers nothing rather than lying.
 */
@Serializable
data class WeighingCapabilitiesDto(
    @SerialName("can_publish") val canPublish: Boolean = false,
    @SerialName("can_end") val canEnd: Boolean = false,
    @SerialName("can_reopen") val canReopen: Boolean = false,
)

@Serializable
data class WeighingCampaignListResponseDto(
    @SerialName("items") val items: List<WeighingCampaignDto> = emptyList(),
    /** Whole-filter task tally behind the two task tabs. Never derived from [items]. */
    @SerialName("counts") val counts: WeighingCampaignCountsDto = WeighingCampaignCountsDto(),
    @SerialName("capabilities") val capabilities: WeighingCapabilitiesDto = WeighingCapabilitiesDto(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("trace_id") val traceId: String? = null,
)

/**
 * The PARK-grain planner vocabulary for ONE weigh date.
 *
 * There is NO cursor here on purpose: a park picker that pages cannot offer the parks it has not
 * reached, which is exactly how one 76-shed park came to be the only selectable park. The catalog
 * carries no shed rows at all; the many side is [WeighingPlannerParkBucketsResponseDto].
 */
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
    /**
     * The park's OWN total of active sheds, counted by the backend over that park's children. It is
     * never a count of rows on a page — the catalog sends no shed rows, and a bucket page carries
     * only ~20 of them.
     */
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("existing_campaign") val existingCampaign: WeighingCampaignSummaryDto? = null,
)

/** ONE keyset page of ONE park's sheds, with the availability answered for the requested date. */
@Serializable
data class WeighingPlannerParkBucketsResponseDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("sheds") val sheds: List<WeighingPlannerShedDto> = emptyList(),
    /** Keyset cursor WITHIN this park. Absent/blank is the last page. */
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("trace_id") val traceId: String? = null,
)

@Serializable
data class WeighingPlannerShedDto(
    @SerialName("location_id") val locationId: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("kid_count") val kidCount: Int = 0,
    // Whether an OPEN weighing task already claims this shed on the REQUESTED weigh date, and who
    // holds it. The server answers this per date; the app never infers availability by scanning its
    // own loaded tasks, which would only ever see the page it happens to hold.
    @SerialName("scheduled") val scheduled: Boolean = false,
    @SerialName("scheduled_campaign_id") val scheduledCampaignId: String = "",
    @SerialName("scheduled_status") val scheduledStatus: String = "",
    @SerialName("scheduled_operator_user_id") val scheduledOperatorUserId: String = "",
    @SerialName("scheduled_operator_display_name") val scheduledOperatorDisplayName: String = "",
    @SerialName("scheduled_weighing_category") val scheduledWeighingCategory: String = "",
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
    /** Parks this person may be assigned in. EMPTY means every park (tenant-scoped). */
    @SerialName("park_ids") val parkIds: List<String> = emptyList(),
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

/**
 * The scope read. FREE-FLOW: there is NO expected-animal roster. The backend dropped
 * `weighing_expected_animals` (000079) and its `items` array is now permanently `[]`, so this DTO
 * deliberately does not bind it -- a client that mapped `items` into Room was writing nothing on
 * every sync. The bucket's own scan/observation history is the whole payload.
 */
@Serializable
data class WeighingRosterResponseDto(
    @SerialName("observations") val observations: List<WeighingAcceptedObservationDto> = emptyList(),
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
    // FREE-FLOW: scanned_identifier is the ONLY identity an accepted observation carries. The
    // backend dropped weighing_observations.animal_id (000078) and never sends the field, so a
    // non-nullable `animalId` fallback here resolved to "" and ERASED the scanned tag.
    @SerialName("scanned_identifier") val scannedIdentifier: String = "",
    @SerialName("weight_kg") val weightKg: Double = 0.0,
    @SerialName("average_weight_kg") val averageWeightKg: Double = 0.0,
    @SerialName("animal_count") val animalCount: Int = 0,
    @SerialName("proof_artifact_id") val proofArtifactId: String = "",
    @SerialName("proof_artifact_ids") val proofArtifactIds: List<String> = emptyList(),
    /** Assigned location from the campaign/shed scope; not a roster expectation. Weighing is free-flow. */
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
    // FREE-FLOW: there is no animal_id. The backend dropped the column (000078) and weighing
    // never resolves a scan to herd identity, so scanned_identifier is the whole identity.
    @SerialName("scanned_identifier") val scannedIdentifier: String? = null,
    @SerialName("weight_kg") val weightKg: Double = 0.0,
    @SerialName("average_weight_kg") val averageWeightKg: Double = 0.0,
    @SerialName("animal_count") val animalCount: Int = 0,
    @SerialName("proof_artifact_id") val proofArtifactId: String = "",
    @SerialName("proof_artifact_ids") val proofArtifactIds: List<String> = emptyList(),
    /** Assigned location from the campaign/shed scope; not a roster expectation. Weighing is free-flow. */
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
    /** The park this bucket's task belongs to. Rendered as the screen eyebrow. */
    @SerialName("park_name") val parkName: String = "",
    /** The task's Asia/Kolkata business DATE. Never a timestamp. */
    @SerialName("weigh_date") val weighDate: String = "",
    /** Who owns this bucket. Blank is the ONLY thing that means nobody is assigned yet. */
    @SerialName("operator_user_id") val operatorUserId: String = "",
    /**
     * Backend-resolved assignee name. Blank WITH a non-blank [operatorUserId] means the assignee has
     * no active workforce record — a roster gap, NOT "not assigned yet".
     */
    @SerialName("operator_display_name") val operatorDisplayName: String = "",
    @SerialName("weighing_category") val weighingCategory: String = "",
    @SerialName("status") val status: String = "",
    /** Backend-owned herd estimate for the shed. A coverage hint, NEVER a completeness denominator. */
    @SerialName("estimated_animal_count") val estimatedAnimalCount: Int = 0,
    /** Backend-owned group-video allowance behind the "N of M" reading. Never a client constant. */
    @SerialName("max_shed_videos") val maxShedVideos: Int = 0,
    @SerialName("individual") val individual: List<WeighingObservationDto> = emptyList(),
    /**
     * Keyset cursor for the next page of [individual], on (accepted_at, observation_id).
     * Absent/blank is the last page. [lumpSum] is a single latest row and is never paged.
     */
    @SerialName("next_individual_cursor") val nextIndividualCursor: String? = null,
    @SerialName("lump_sum") val lumpSum: WeighingObservationDto? = null,
    /** Backend-owned sentence for the weigh period. The client never builds it from two dates. */
    @SerialName("period_label") val periodLabel: String = "",
)

/**
 * One keyset page of shed buckets across tasks (`GET /app/weighing/leadership/sheds`) — the
 * leadership gallery read.
 *
 * Each item carries its own context AND its first page of evidence, so the gallery is ONE request.
 * It replaced a client fan-out that expanded a task page into every embedded bucket and then called
 * the single-bucket read once per bucket.
 */
@Serializable
data class WeighingLeadershipShedPageResponseDto(
    @SerialName("items") val items: List<WeighingLeadershipShedVideosDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("trace_id") val traceId: String? = null,
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
