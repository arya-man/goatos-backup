package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Generic, module-agnostic video-verification queue (context/architecture/
 * verification-module-design.md + context/architecture/verifier-app-and-flow.md).
 * A producer module (vaccination, diagnosis, death/post-mortem, feed-direction,
 * breeding, …) emits a `verification_item`; the standalone mobile Verifier section
 * renders it category-filtered and lets the verifier approve or reject + reason.
 *
 * DTOs reconciled against `contracts/openapi/app-api.yaml` (GET /verification/queue,
 * POST /verification/items/{id}/verdict). ignoreUnknownKeys on the shared
 * [sg.mesha.goatos.core.network.dto] json config makes this forward-compatible
 * with fields the backend adds later.
 */

/** Back-pointer to the module/task/submission that produced this verification item. */
@Serializable
data class VerificationSourceRef(
    @SerialName("module") val module: String = "",
    @SerialName("ref_type") val refType: String = "",
    @SerialName("ref_id") val refId: String = "",
    @SerialName("task_id") val taskId: String? = null,
    @SerialName("submission_id") val submissionId: String? = null,
)

/**
 * One piece of proof media on a verification item. [downloadUrl] is a short-lived streamed
 * URL (never proxied through the app-api, never persisted past its TTL — see the media
 * verification-module-design.md §2.5 scale rule) that a video player streams directly.
 */
@Serializable
data class VerificationMediaItem(
    @SerialName("proof_id") val proofId: String = "",
    @SerialName("label") val label: String? = null,
    @SerialName("answer") val answer: String? = null,
    @SerialName("download_url") val downloadUrl: String = "",
    @SerialName("mime_type") val mimeType: String? = null,
    @SerialName("duration_ms") val durationMs: Long? = null,
)

/**
 * The correctable-measurement control for one verification item, declared per category by the
 * producing module. Rendered VERBATIM: this app composes none of these words, so a phone and an
 * admin-web drawer showing the same control cannot word it differently.
 */
@Serializable
data class VerificationMeasurementCorrectionDto(
    /** Echoed from the item's source.ref_type. Posted back verbatim; never inferred. */
    @SerialName("ref_type") val refType: String = "",
    /** Echoed from the item's source.ref_id -- the record the correction addresses. */
    @SerialName("observation_id") val observationId: String = "",
    @SerialName("title") val title: String = "",
    /** Says plainly that the value REPLACES the recorded one. Render it; never paraphrase it. */
    @SerialName("help") val help: String = "",
    /** Label for the number itself, unit included. */
    @SerialName("value_label") val valueLabel: String = "",
    /**
     * Label for the separate save control that used to sit under the field. THAT CONTROL IS GONE
     * (maintainer decision 2026-08-20): the verifier types the number and presses Approve, and the
     * approve carries it. Kept so an older payload still decodes and so an installed build that
     * still shows its own button keeps its copy. Nothing in this app renders it any more.
     */
    @SerialName("submit_label") val submitLabel: String = "",
    /**
     * Keep Approve DISABLED until a number is entered.
     *
     * True for feed wastage, where the operator submits a video only and the reading is born on the
     * verifier's screen -- approving without one completes a pen-day with no wastage recorded at
     * all. False for weighing, where the operator already recorded a weight and a blank field means
     * "his weight is right", the normal case, which stays a single tap.
     *
     * Defaults FALSE so an older payload decodes to the permissive behaviour rather than locking
     * Approve on every measurable item.
     */
    @SerialName("required_for_approve") val requiredForApprove: Boolean = false,
    /**
     * Label for an accompanying whole-number field (a lump-sum shed proof's head count). Null or
     * blank means render the value field ALONE -- an individual animal's proof carries no count and
     * the write path refuses one, so offering the field there would invite a rejected value.
     */
    @SerialName("count_label") val countLabel: String? = null,
    /**
     * The ordered per-item entry-box list for items whose approve carries one value PER FIELD --
     * a feed packing item: one box per feed item of that pen-session, NAMES ONLY (the planned
     * quantities are deliberately hidden so the verifier enters blind; maintainer decision
     * 2026-08-21). Non-empty means render one labelled numeric box per field INSTEAD of the single
     * value field, keep Approve disabled until every box is filled, and send the verdict's
     * measurement as `entries` echoing each field's key. Empty means the single-value contract.
     */
    @SerialName("fields") val fields: List<VerificationMeasurementFieldDto> = emptyList(),
)

/** One per-item entry box: the producer's stable key posted back verbatim, and its caption. */
@Serializable
data class VerificationMeasurementFieldDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
)

/**
 * One row in the verifier's queue. The backend owns all display composition;
 * the app never derives labels from ids (TRD dumb-renderer rule). [category]
 * is the module-agnostic filter dimension (e.g. `vaccine`, `feed_direction`,
 * `diagnosis`, `death_post_mortem`, `breeding`, …) driven by the backend's
 * verification type registry, never a client-hardcoded enum.
 */
@Serializable
data class VerificationQueueItem(
    @SerialName("item_id") val itemId: String = "",
    @SerialName("vertical") val vertical: String = "",
    @SerialName("module") val module: String = "",
    @SerialName("category") val category: String = "",
    @SerialName("subject_label") val subjectLabel: String? = null,
    /** The raiser's own note about this work item (e.g. why a movement was requested). */
    @SerialName("subject_note") val subjectNote: String? = null,
    /**
     * What the reviewed work was EXPECTED to be -- for a feed packing proof, the frozen ration for
     * that pen-session. Backend-composed label/value pairs in the producer's order, rendered
     * VERBATIM: never parsed, reordered or re-labelled, and never switched on by label, because a
     * producer may add rows at any time. Defaulted so an older payload still decodes.
     */
    @SerialName("context_rows") val contextRows: List<VerificationContextRowDto> = emptyList(),
    /**
     * Backend-owned declaration that this item carries a number the VERIFIER may correct while
     * reviewing the proof, plus every word of that control's copy (maintainer decision 2026-08-17).
     * Null -- every category but weighing today -- means the screen renders no correction control.
     *
     * It carries NO current value: the number is already in [subjectLabel], which the producing
     * module composes and she is reading while she watches the video. Defaulted so an older payload
     * still decodes.
     */
    @SerialName("measurement_correction") val measurementCorrection: VerificationMeasurementCorrectionDto? = null,
    @SerialName("status") val status: String = "",
    @SerialName("captured_at") val capturedAt: String = "",
    @SerialName("row_version") val rowVersion: Int = 1,
    @SerialName("media") val media: List<VerificationMediaItem> = emptyList(),
    @SerialName("source") val source: VerificationSourceRef = VerificationSourceRef(),
    @SerialName("verdict_reason") val verdictReason: String? = null,
    @SerialName("operator_id") val operatorId: String? = null,
    @SerialName("operator_name") val operatorName: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("partition_label") val partitionLabel: String? = null,
    // Render THIS verbatim -- the backend composes it (shed + partition). Reading shedLabel
    // instead is what kept a partitioned shed reading bare "Godel 1" after the wire was fixed.
    @SerialName("operational_location_display") val operationalLocationDisplay: String? = null,
    @SerialName("shed_label") val shedLabel: String? = null,
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_label") val parkLabel: String? = null,
    @SerialName("verified_by") val verifiedBy: String? = null,
    @SerialName("verified_by_name") val verifiedByName: String? = null,
    @SerialName("verified_at") val verifiedAt: String? = null,
    @SerialName("closed_by") val closedBy: String? = null,
    @SerialName("closed_at") val closedAt: String? = null,
    /** R50-017: false when the backend's evidence resolver could not produce a signed/servable
     *  proof for this item (missing object or signing failure) — the backend now fails that
     *  resolution closed rather than silently omitting media, so the app must disable a verdict
     *  decision instead of letting the verifier approve/reject on no evidence. Defaults `true`
     *  for forward/backward compatibility with a backend that has not shipped this field yet. */
    @SerialName("evidence_available") val evidenceAvailable: Boolean = true,
)

/** GET /verification/queue — keyset-paginated, category-filtered (~20/page, TRD §14). */
@Serializable
data class VerificationQueueResponseDto(
    @SerialName("items") val items: List<VerificationQueueItem> = emptyList(),
    @SerialName("filter_options") val filterOptions: VerificationFilterOptionsDto = VerificationFilterOptionsDto(),
    @SerialName("drive_closures") val driveClosures: List<VerificationDriveClosureDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class VerificationFilterOptionsDto(
    @SerialName("module_key") val moduleKey: String = "",
    @SerialName("module_label") val moduleLabel: String = "",
    @SerialName("pages") val pages: List<VerificationPageOptionDto> = emptyList(),
    @SerialName("statuses") val statuses: List<VerificationStatusOptionDto> = emptyList(),
    @SerialName("parks") val parks: List<VerificationLocationOptionDto>? = null,
    @SerialName("sheds") val sheds: List<VerificationLocationOptionDto>? = null,
    @SerialName("selected_business_date") val selectedBusinessDate: String? = null,
    @SerialName("business_timezone") val businessTimezone: String = "Asia/Kolkata",
    @SerialName("missed_only") val missedOnly: Boolean = false,
    @SerialName("has_missed") val hasMissed: Boolean = false,
)

@Serializable
data class VerificationPageOptionDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("category") val category: String = "",
)

@Serializable
data class VerificationStatusOptionDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("status") val status: String = "",
)

@Serializable
data class VerificationLocationOptionDto(
    @SerialName("id") val id: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String? = null,
)

@Serializable
data class VerificationDriveClosureDto(
    @SerialName("batch_id") val batchId: String = "",
    @SerialName("drive_key") val driveKey: String = "",
    @SerialName("drive_label") val driveLabel: String = "",
    @SerialName("batch_label") val batchLabel: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("start_date") val startDate: String = "",
    @SerialName("end_date") val endDate: String = "",
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("approved_count") val approvedCount: Int = 0,
    @SerialName("rejected_count") val rejectedCount: Int = 0,
    @SerialName("pending_count") val pendingCount: Int = 0,
    @SerialName("video_count") val videoCount: Int = 0,
    @SerialName("approved_videos") val approvedVideos: Int = 0,
    @SerialName("rejected_videos") val rejectedVideos: Int = 0,
    @SerialName("pending_videos") val pendingVideos: Int = 0,
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("ready") val ready: Boolean = false,
    /** True once the drive has been closed. Closed drives are STILL returned so the card can show a
     *  read-only "Closed" state instead of vanishing -- a card that disappears on success gives
     *  leadership no confirmation the close happened, and nothing at all after an app relaunch. */
    @SerialName("closed") val closed: Boolean = false,
    /** Backend-formatted Asia/Kolkata date the drive was closed (e.g. "08 Aug 2026"). */
    @SerialName("closed_at") val closedAt: String? = null,
)

/** Request body for POST /verification/items/{item_id}/verdict. [reason] is mandatory for a
 *  `rejected` decision (enforced client-side before enqueue AND server-side); optional for
 *  `approved`. [rowVersion] is the optimistic-concurrency guard against a stale verdict. */
@Serializable
data class VerificationVerdictRequestDto(
    @SerialName("decision") val decision: String,
    @SerialName("reason") val reason: String? = null,
    @SerialName("row_version") val rowVersion: Int,
    /**
     * THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20). The reading the verifier
     * took off the video, applied by the backend in the same act as the verdict.
     *
     * Null is the normal weighing case -- blank means the operator's recorded weight is right.
     * Send it ONLY on an approve of an item whose `measurement_correction` is present; the backend
     * drops it on a reject, because rejection sends the work back to be recorded again.
     */
    @SerialName("measurement") val measurement: VerificationVerdictMeasurementDto? = null,
)

/**
 * The verifier's reading, carried by her approve.
 *
 * It names NO target. The record it lands on comes from the item's own source, resolved by the
 * backend -- a client that could name its own target could aim one item's approve at another
 * item's record.
 */
@Serializable
data class VerificationVerdictMeasurementDto(
    /**
     * The single-value reading in the category's own unit (kg for weighing and wastage).
     *
     * ZERO IS VALID for wastage -- an empty trough is a real measurement -- so "she typed nothing"
     * is carried by a NULL measurement block, never by a 0 in this field. Null on a per-field item
     * (feed packing), whose readings travel on [entries] instead.
     */
    @SerialName("value") val value: Double? = null,
    /**
     * One reading per measurement_correction.fields entry, keys echoed VERBATIM. Every declared
     * field must be filled for the approve to land; the backend refuses a partial set and a key
     * the item never declared. ZERO IS VALID -- "this item was not packed" is a real observation.
     */
    @SerialName("entries") val entries: List<VerificationVerdictMeasurementEntryDto> = emptyList(),
    /**
     * The accompanying whole-number field, allowed only where the item's correction carries a
     * count label (a lump-sum shed weigh's head count). Null leaves the recorded count alone,
     * which is the normal case. Sending one where the item carries none is REFUSED by the backend
     * rather than dropped, so it is omitted at the grain that cannot carry it.
     */
    @SerialName("count") val count: Int? = null,
    @SerialName("reason") val reason: String? = null,
)

/** One filled entry box on an approve: the field's key plus the reading. */
@Serializable
data class VerificationVerdictMeasurementEntryDto(
    @SerialName("key") val key: String,
    @SerialName("value") val value: Double,
)

@Serializable
data class VerificationVerdictResponseDto(
    @SerialName("item") val item: VerificationQueueItem = VerificationQueueItem(),
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class VerificationCloseRequestDto(
    @SerialName("row_version") val rowVersion: Int,
)

@Serializable
data class VerificationCloseSubmissionResponseDto(
    @SerialName("items") val items: List<VerificationQueueItem> = emptyList(),
    @SerialName("trace_id") val traceId: String = "",
)

/** POST /verification/review-events -- verifier-only backend audit stream. */
@Serializable
data class VerificationReviewEventBatchRequestDto(
    @SerialName("events") val events: List<VerificationReviewEventRequestDto> = emptyList(),
)

@Serializable
data class VerificationReviewEventRequestDto(
    @SerialName("item_id") val itemId: String? = null,
    @SerialName("proof_id") val proofId: String? = null,
    @SerialName("session_id") val sessionId: String = "",
    @SerialName("event_type") val eventType: String = "",
    @SerialName("occurred_at") val occurredAt: String = "",
    @SerialName("payload") val payload: VerificationReviewEventPayloadDto = VerificationReviewEventPayloadDto(),
    @SerialName("client_event_id") val clientEventId: String = "",
)

@Serializable
data class VerificationReviewEventPayloadDto(
    @SerialName("video_position_ms") val videoPositionMs: Long? = null,
    @SerialName("video_duration_ms") val videoDurationMs: Long? = null,
    @SerialName("seek_from_ms") val seekFromMs: Long? = null,
    @SerialName("seek_to_ms") val seekToMs: Long? = null,
    @SerialName("verdict") val verdict: String? = null,
    @SerialName("category") val category: String? = null,
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("status") val status: String? = null,
)

@Serializable
data class VerificationReviewEventBatchResponseDto(
    @SerialName("inserted") val inserted: Int = 0,
    @SerialName("trace_id") val traceId: String = "",
)

/** [VerificationVerdictRequestDto.decision] values — never inline string-literal-compared. */
object VerificationDecision {
    const val APPROVED = "approved"
    const val REJECTED = "rejected"
}

/** [VerificationQueueItem.status] values. */
object VerificationStatus {
    const val PENDING = "pending"
    const val APPROVED = "approved"
    const val REJECTED = "rejected"
}

/** One backend-composed "what was expected" line on a verification item. */
@Serializable
data class VerificationContextRowDto(
    @SerialName("label") val label: String = "",
    @SerialName("value") val value: String = "",
)


/**
 * THE VERIFIER'S WEIGHT CORRECTION (maintainer decision 2026-08-17).
 *
 * She watches the proof video and replaces the number the operator typed. The corrected value
 * REPLACES the recorded one: on an individual capture that one animal's weight, on a lump-sum
 * capture the shed total plus optionally the head count.
 */
@Serializable
data class WeighingWeightCorrectionRequestDto(
    /** The observation grain, echoed from the item's measurement_correction.ref_type. */
    @SerialName("ref_type") val refType: String,
    @SerialName("weight_kg") val weightKg: Double,
    /**
     * LUMP-SUM ONLY, and omitted entirely (not sent as 0) when she left it blank -- blank means
     * "leave the recorded count alone". The backend REFUSES a head count on an individual capture
     * rather than ignoring it, so sending 0 there would fail the whole correction.
     */
    @SerialName("animal_count") val animalCount: Int? = null,
    @SerialName("reason") val reason: String? = null,
    @SerialName("idempotency_key") val idempotencyKey: String? = null,
)

@Serializable
data class WeighingWeightCorrectionResponseDto(
    @SerialName("weight_correction") val weightCorrection: WeighingWeightCorrectionResultDto? = null,
)

@Serializable
data class WeighingWeightCorrectionResultDto(
    @SerialName("observation_id") val observationId: String = "",
    @SerialName("ref_type") val refType: String = "",
    @SerialName("campaign_shed_id") val campaignShedId: String = "",
    /** The weight AFTER the correction. */
    @SerialName("weight_kg") val weightKg: Double = 0.0,
    @SerialName("animal_count") val animalCount: Int = 0,
    @SerialName("average_weight_kg") val averageWeightKg: Double = 0.0,
    @SerialName("previous_weight_kg") val previousWeightKg: Double = 0.0,
    /**
     * The recomposed verifier-facing sentence carrying the corrected weight. The backend pushes it
     * onto the verification item too; rendered verbatim, never composed here.
     */
    @SerialName("subject_label") val subjectLabel: String = "",
    @SerialName("corrected_at") val correctedAt: String = "",
)
