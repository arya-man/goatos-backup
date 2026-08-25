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
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
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
    @SerialName("planned_business_date") val plannedBusinessDate: String = "",
    @SerialName("due_business_date") val dueBusinessDate: String = "",
    /** Backend-owned count of animals still open in THIS shed bucket. Absent when the
     *  backend read model does not publish shed-grain remaining truth yet; the client
     *  must then render a count-free close label instead of inventing a number. */
    @SerialName("remaining_count") val remainingCount: Int? = null,
    /** Backend-owned count of this bucket's submitted evidence still awaiting a verifier look. */
    @SerialName("pending_verification_count") val pendingVerificationCount: Int = 0,
    /** Strict subset of [pendingVerificationCount] a verifier bounced back to the operator. */
    @SerialName("rework_count") val reworkCount: Int = 0,
    @SerialName("latest_rework_reason") val latestReworkReason: String = "",
    /** True only when this bucket is submitted and every observation on it is verified. */
    @SerialName("ready_to_close") val readyToClose: Boolean = false,
    /**
     * FACT 1 of 2. Backend-owned count of the ANIMALS this bucket has a RECORDED weight for,
     * submitted or not: one per individual observation, plus the recorded head count of the
     * standing lump-sum proof.
     *
     * A plain count, NEVER a numerator: weighing is free-flow, so there is no expected-animal
     * total to divide it by. Render it as-is and never as a percentage or a progress-bar fill.
     */
    @SerialName("animals_weighed_count") val animalsWeighedCount: Int = 0,
    /**
     * FACT 2 of 2. The subset of [animalsWeighedCount] that has been SUBMITTED for verification.
     *
     * Always rendered WITH the weighed count, as "N weighed · N submitted" — never alone. Mid-shift
     * the two legitimately differ (weighed 3, submitted 0), and that gap is exactly where work is
     * silently lost when an operator walks away; a single number cannot say it.
     */
    @SerialName("animals_submitted_count") val animalsSubmittedCount: Int = 0,
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

/**
 * ONE task resolved by id (`GET /app/weighing/campaigns/{campaign_id}`).
 *
 * Separate from [WeighingCampaignResponseDto], which answers the create/update/publish WRITES and
 * carries no capabilities: this read is what a notification deep link resolves against, so it has
 * to state what the tapper may do to the task it just opened.
 */
@Serializable
data class WeighingCampaignDetailResponseDto(
    @SerialName("campaign") val campaign: WeighingCampaignDto = WeighingCampaignDto(),
    @SerialName("capabilities") val capabilities: WeighingCapabilitiesDto = WeighingCapabilitiesDto(),
    @SerialName("trace_id") val traceId: String? = null,
)

/**
 * One park the caller may filter weighing by (`GET /app/weighing/parks`). Identity ONLY: anything
 * date-scoped or count-bearing is planner-catalog grain and sits behind the planner permission.
 */
@Serializable
data class WeighingParkDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("name") val name: String = "",
)

/**
 * The caller's authorized weighing parks. UNPAGED on purpose -- a chip row cannot offer the parks
 * it has not paged to, which is the exact defect that made the chips a function of loaded rows.
 */
@Serializable
data class WeighingParkListResponseDto(
    @SerialName("parks") val parks: List<WeighingParkDto> = emptyList(),
    @SerialName("trace_id") val traceId: String? = null,
)

/**
 * What ONE person's weighing work adds up to, as the BACKEND counts it.
 *
 * Every field is a plain count and none is ever a numerator: weighing is free-flow, there is no
 * expected-animal roster, so no share or percentage can honestly be built from any of these.
 * [notStartedCount] + [capturingCount] + [submittedCount] + [acceptedCount] == [shedCount]
 * exactly — the four are disjoint and exhaustive over the live bucket statuses — which is what
 * lets the oversight screen draw a DISCRETE state ladder instead of an invented fraction.
 *
 * The counts range over the whole park filter, not over a loaded page, so they do not move as the
 * reader scrolls. The client must never rebuild them by grouping shed rows it happens to hold.
 */
@Serializable
data class WeighingOperatorSummaryDto(
    /** Empty on the "nobody is assigned yet" row, which is real work leadership must see. */
    @SerialName("operator_user_id") val operatorUserId: String = "",
    /**
     * Backend-resolved name. Blank WITH a non-blank [operatorUserId] is a roster gap, NOT
     * "unassigned"; the screen names the gap and never falls back to rendering a user id.
     */
    @SerialName("operator_display_name") val operatorDisplayName: String = "",
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("not_started_count") val notStartedCount: Int = 0,
    @SerialName("capturing_count") val capturingCount: Int = 0,
    @SerialName("submitted_count") val submittedCount: Int = 0,
    @SerialName("accepted_count") val acceptedCount: Int = 0,
    /** Buckets a verifier bounced back. OVERLAPS the four state counts; never added to them. */
    @SerialName("rework_count") val reworkCount: Int = 0,
    /**
     * How many ANIMALS this person's buckets account for: one per individual observation plus the
     * recorded head count of a standing lump-sum weighing. A plain total of work done.
     */
    @SerialName("animals_weighed_count") val animalsWeighedCount: Int = 0,
    /**
     * The subset of [animalsWeighedCount] this person has SUBMITTED for verification. Rendered
     * with the weighed count as "N weighed · N submitted"; the same two facts, with the same two
     * definitions, that the per-bucket task detail shows.
     */
    @SerialName("animals_submitted_count") val animalsSubmittedCount: Int = 0,
)

@Serializable
data class WeighingCampaignListResponseDto(
    @SerialName("items") val items: List<WeighingCampaignDto> = emptyList(),
    /** Whole-filter task tally behind the two task tabs. Never derived from [items]. */
    @SerialName("counts") val counts: WeighingCampaignCountsDto = WeighingCampaignCountsDto(),
    /** Backend-owned OPERATOR-grain roll-up behind the oversight surface. Never derived from [items]. */
    @SerialName("operator_summaries") val operatorSummaries: List<WeighingOperatorSummaryDto> = emptyList(),
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
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
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
    @SerialName("partition_label") val partitionLabel: String? = null,
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
    /**
     * The verifier's verdict for THIS capture, and why if it came back. "rework" is the state
     * the operator has to act on; without it a rejected animal restores from the server looking
     * exactly like an accepted one.
     */
    @SerialName("verification_status") val verificationStatus: String? = null,
    @SerialName("rework_reason") val reworkReason: String? = null,
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
    // The head count and average stopped being client inputs on 2026-08-24: the
    // backend snapshots the count from the herd register at submit and derives
    // the average itself. Nullable-with-default so a NEW submit omits both while
    // an OLD queued outbox row (which recorded them) still decodes and replays.
    @SerialName("animal_count") val animalCount: Int? = null,
    @SerialName("average_weight_kg") val averageWeightKg: Double? = null,
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
    @SerialName("actual_partition_label") val actualPartitionLabel: String? = null,
    @SerialName("accepted_at") val acceptedAt: String = "",
    @SerialName("media") val media: List<WeighingProofMediaDto> = emptyList(),
    /**
     * Which tag a verifier sent back, and why. Without these the capture list renders a rejected
     * animal identically to an accepted one — green tick, "Video synced" — so the operator only
     * discovers the rejection when Submit refuses the whole shed.
     */
    @SerialName("verification_status") val verificationStatus: String? = null,
    @SerialName("rework_reason") val reworkReason: String? = null,
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
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
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

/**
 * ONE weighing work-state transition that was routed to the caller.
 *
 * Every human-visible string here is BACKEND-AUTHORED and rendered verbatim: [title], [body] and
 * (on the page) the screen title and empty-state sentence. The app composes no weighing copy of
 * its own -- see contracts/openapi/app-api.yaml#WeighingAlert.
 *
 * Weighing is free-flow and fully herd-isolated, so no field here names a goat, carries a herd
 * identity, or reports a share of an expected roster. A row identifies a shed, a bucket, a
 * campaign and a park -- nothing below that.
 */
@Serializable
data class WeighingAlertDto(
    @SerialName("alert_id") val alertId: String = "",
    /** Producer's transition type (weighing_campaign_published, weighing_shed_submitted, ...).
     *  Safe to branch on for iconography; NEVER rebuild the sentence from it. */
    @SerialName("kind") val kind: String = "",
    /** "downstream" (landed on the person who must act) or "upstream" (on the person who
     *  oversees). Which way this alert travelled FOR THIS RECIPIENT. */
    @SerialName("direction") val direction: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("body") val body: String = "",
    /** "normal" or "high". */
    @SerialName("severity") val severity: String = "",
    /** In-app destination this row opens, chosen by the backend (e.g. "/weighing"). */
    @SerialName("target") val target: String = "",
    @SerialName("shed_label") val shedLabel: String? = null,
    @SerialName("campaign_id") val campaignId: String? = null,
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("occurred_at") val occurredAt: String = "",
)

/** ONE keyset page of the caller's weighing alerts, newest first. */
@Serializable
data class WeighingAlertPageResponseDto(
    @SerialName("items") val items: List<WeighingAlertDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    /** Backend-owned screen title. Render this; do not hardcode a weighing string. */
    @SerialName("title") val title: String = "",
    /** Backend-owned empty-state sentence, shown when [items] is empty. */
    @SerialName("empty_message") val emptyMessage: String = "",
    @SerialName("trace_id") val traceId: String? = null,
)
