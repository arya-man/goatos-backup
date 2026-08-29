package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.network.dto.MilkFeedingAnswersDto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.network.dto.ClockPunchRequestDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.FeedWastageMeasurementRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationReviewEventBatchRequestDto
import sg.mesha.goatos.core.network.dto.WeighingWeightCorrectionRequestDto
import sg.mesha.goatos.core.network.dto.VerificationCloseRequestDto
import sg.mesha.goatos.core.network.dto.WeighingAnimalObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeSubmitRequestDto
import sg.mesha.goatos.core.network.dto.WeighingShedObservationRequestDto

/** Shared JSON codec for outbox payload/result blobs — lenient so a field added later never
 *  breaks decode of an already-queued row (mirrors [sg.mesha.goatos.core.data.BootstrapCache]). */
internal val syncJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
}

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SHED_SUBMIT]. Reuses
 *  the existing wire DTO directly rather than duplicating its shape. */
@Serializable
data class ShedSubmitPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("request") val request: SubmitTaskRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SCAN_CAPTURE].
 *  Draft RFID scan capture that is synced before final Submit. */
@Serializable
data class ScanCapturePayload(
    @SerialName("task_id") val taskId: String,
    /** Local Room identity only; it is not part of the backend scan-capture request body. */
    @SerialName("partition_key") val partitionKey: String = "whole",
    @SerialName("request") val request: ScanCaptureRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SCAN_ATTEMPT].
 *  Append-only RFID reader audit event; does not affect Submit counters. */
@Serializable
data class ScanAttemptPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("request") val request: ScanAttemptRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.RESCHEDULE]. */
@Serializable
data class ReschedulePayload(
    @SerialName("obligation_id") val obligationId: String,
    @SerialName("request") val request: RescheduleObligationRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.PROOF_UPLOAD].
 *  [localFilePath] is the captured video's location in this app's OWN private storage
 *  (`Context.filesDir` — see `InAppVideoRecorder`); [SyncEngine.dispatchProofUpload] streams it
 *  to the signed URL `registerProof` returns (docs/mobile/proof-capture-sync-and-e2e.md §3). */
@Serializable
data class ProofUploadPayload(
    @SerialName("request") val request: ProofUploadRequestDto,
    @SerialName("local_file_path") val localFilePath: String = "",
    @SerialName("duration_ms") val durationMs: Long? = null,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.VERIFY_TASK].
 *  Leadership verify action on a record task (C35-011). */
@Serializable
data class VerifyTaskPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("request") val request: ReviewTaskRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.REWORK_TASK].
 *  Leadership rework action on a record task (C35-011). */
@Serializable
data class ReworkTaskPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("request") val request: ReviewTaskRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.VERIFICATION_VERDICT].
 *  The standalone Verifier section's approve/reject + reason action
 *  (context/architecture/verifier-app-and-flow.md). */
@Serializable
data class VerificationVerdictPayload(
    @SerialName("item_id") val itemId: String,
    @SerialName("request") val request: VerificationVerdictRequestDto,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.WEIGHING_WEIGHT_CORRECTION]:
 * the VERIFIER replacing the weight the operator typed, while she watches the proof video
 * (maintainer decision 2026-08-17).
 *
 * The observation id and ref type come from the item's backend-owned measurement_correction block,
 * which echoes source.ref_id/source.ref_type -- this app never composes that address itself.
 */
@Serializable
data class WeighingWeightCorrectionPayload(
    @SerialName("observation_id") val observationId: String,
    @SerialName("request") val request: WeighingWeightCorrectionRequestDto,
)

@Serializable
data class VerificationClosePayload(
    @SerialName("item_id") val itemId: String,
    @SerialName("request") val request: VerificationCloseRequestDto,
)

@Serializable
data class VerificationCloseSubmissionPayload(
    @SerialName("submission_id") val submissionId: String,
)

@Serializable
data class VerificationCloseBatchPayload(
    @SerialName("batch_id") val batchId: String,
)

/** Durable verifier journey audit batch. Each nested event carries its persisted client_event_id. */
@Serializable
data class VerificationReviewEventsPayload(
    @SerialName("request") val request: VerificationReviewEventBatchRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.COUNTS_SHIFTING].
 *  An operator-reported movement between sheds. The destination shed is the outbox GROUP KEY, so
 *  two movements into the same shed drain strictly in the order they were recorded. */
@Serializable
data class CountsShiftingPayload(
    @SerialName("request") val request: CountsShiftingEventRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.COUNTS_BIRTH].
 *  A birth is goat creation; the backend pins `origin_type` to `birth`. */
@Serializable
data class CountsBirthPayload(
    @SerialName("request") val request: CountsBirthEventRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.COUNTS_DEATH].
 *  A death recorded through identity's guardrailed critical-death exit; the animal is the outbox
 *  GROUP KEY so two writes about the same goat can never drain out of order. */
@Serializable
data class CountsDeathPayload(
    @SerialName("request") val request: CountsDeathEventRequestDto,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.COUNTS_APPROVAL_APPROVE]
 * and [sg.mesha.goatos.core.database.outbox.OutboxOpType.COUNTS_APPROVAL_REJECT].
 *
 * The APPROVAL REQUEST ID is the outbox group key, so two decisions addressing the same request can
 * never drain concurrently or out of order — the second would otherwise race the first and hit a
 * 409 for a request the approver believes they only decided once.
 *
 * One payload type serves both decisions because the bodies are identical; the op type is what
 * selects the endpoint, which keeps an approve and a reject on the same request from ever sharing
 * a request fingerprint.
 */
@Serializable
data class CountsApprovalDecisionPayload(
    @SerialName("request_id") val requestId: String,
    @SerialName("request") val request: CountsApprovalDecisionRequestDto,
)

@Serializable
data class WeighingAnimalObservationPayload(
    @SerialName("campaign_id") val campaignId: String,
    @SerialName("request") val request: WeighingAnimalObservationRequestDto,
)

@Serializable
data class WeighingShedObservationPayload(
    @SerialName("campaign_id") val campaignId: String,
    @SerialName("request") val request: WeighingShedObservationRequestDto,
)

/** Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.WEIGHING_SCOPE_SUBMIT]. */
@Serializable
data class WeighingScopeSubmitPayload(
    @SerialName("campaign_id") val campaignId: String,
    @SerialName("campaign_shed_id") val campaignShedId: String,
    @SerialName("request") val request: WeighingScopeSubmitRequestDto,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SHIFTING_COMPLETE].
 *
 * The "Mark done" that may RELOCATE the animals when Park Head approval is already present. The
 * SHIFTING EVENT ID is the outbox group key, so actions on the same movement cannot drain
 * concurrently or out of order. The target stage was snapshotted when the movement was raised.
 *
 * Every movement couples its mandatory shifting video to this completion. High-priority movements
 * additionally couple mandatory feed-packing and feed-given videos plus the destination Feed Config
 * fingerprint shown to the operator. All proof uploads drain before this completion, and the backend
 * rejects changed or unavailable feed configuration instead of accepting guessed feed details.
 */
@Serializable
data class ShiftingCompletePayload(
    @SerialName("shifting_event_id") val shiftingEventId: String,
    @SerialName("destination_tag") val destinationTag: String? = null,
    /**
     * Outbox id of the MANDATORY video's PROOF_UPLOAD item (maintainer decision, 2026-07-26). The
     * dispatcher resolves this item's uploaded proof_id and sends it as `proof_ref`; the second of
     * Park Head approval and operator completion applies the move. Enqueued on the same group as this
     * completion, so it drains first. Optional-nullable only for backward decode of any pre-upgrade
     * queued row.
     */
    @SerialName("proof_outbox_item_id") val proofOutboxItemId: String? = null,
    /** Required together with [feedGivenProofOutboxItemId] for a high-priority movement. */
    @SerialName("feed_packing_proof_outbox_item_id") val feedPackingProofOutboxItemId: String? = null,
    /** Required together with [feedPackingProofOutboxItemId] for a high-priority movement. */
    @SerialName("feed_given_proof_outbox_item_id") val feedGivenProofOutboxItemId: String? = null,
    /** Semantic fingerprint of the exact active feed requirement rendered for a high task. */
    @SerialName("feed_config_fingerprint") val feedConfigFingerprint: String? = null,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.SHIFTING_CANCEL]. Retires an
 * authorized movement that will never be walked; moves nothing. [reason] is REQUIRED server-side.
 */
@Serializable
data class ShiftingCancelPayload(
    @SerialName("shifting_event_id") val shiftingEventId: String,
    @SerialName("reason") val reason: String,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.COUNTS_PROMOTE_IDENTIFIER].
 * Assigns a permanent RFID to a temporary-tagged goat, atomically retiring the temp tag. The temp
 * tag to retire is found server-side, so only the [permanentIdentifier] and the goat's [rowVersion]
 * are carried; [goatId] is the path target and the outbox group key.
 */
@Serializable
data class PromoteIdentifierPayload(
    @SerialName("goat_id") val goatId: String,
    @SerialName("permanent_identifier") val permanentIdentifier: String,
    // Optional second permanent RFID (animal_identifier_2). Null = attach only the primary.
    @SerialName("animal_identifier_2") val animalIdentifier2: String? = null,
    @SerialName("row_version") val rowVersion: Int,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.FEED_DIRECTION_COMPLETE].
 * Records that one shed-session's feed direction was carried out. Carries only the shed-session
 * identity -- the OPTIONAL video is NOT here; it flows separately through PROOF_UPLOAD, exactly like
 * [ShiftingCompletePayload].
 */
@Serializable
data class FeedDirectionCompletePayload(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String,
    @SerialName("session_no") val sessionNo: Int,
    @SerialName("target_date") val targetDate: String,
    @SerialName("workflow") val workflow: String,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.FEED_DISTRIBUTION_COMPLETE]
 * — the verifier-GATED direction flow (docs/decisions/feed-distribution-verification.md). Unlike
 * [FeedDirectionCompletePayload], BOTH proofs are MANDATORY and carried by reference to their
 * PROOF_UPLOAD outbox rows (exactly like [ShiftingCompletePayload]'s single mandatory video): the
 * dispatcher resolves each row's uploaded `proof_id` and sends the set as `feed_weight_proof_ref` /
 * `distribution_proof_ref` / `water_proof_ref`. All three proof uploads are enqueued on the SAME
 * group as this completion, so they drain first. The shed-session key is the outbox group key.
 */
@Serializable
data class FeedDistributionCompletePayload(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String,
    /** The PEN worked; null for an undivided shed. Defaulted so an outbox row written by an older
     *  build still decodes — it simply predates per-pen completions. */
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("session_no") val sessionNo: Int,
    @SerialName("target_date") val targetDate: String,
    @SerialName("workflow") val workflow: String,
    /**
     * Outbox id of the MANDATORY feed-weight PHOTO's PROOF_UPLOAD item.
     *
     * NULLABLE with a default ONLY so a row queued by a build that predates the 2026-08-11 weight
     * photo still DECODES. It is not optional: the dispatcher fails such a row terminally with an
     * operator-facing reason rather than sending an incomplete set the backend would reject forever.
     * A non-null field here would throw at decode time and strand the row with no message at all.
     */
    @SerialName("feed_weight_proof_outbox_item_id") val feedWeightProofOutboxItemId: String? = null,
    /** Outbox id of the MANDATORY feed-distribution VIDEO's PROOF_UPLOAD item. */
    @SerialName("distribution_proof_outbox_item_id") val distributionProofOutboxItemId: String? = null,
    /** Outbox id of the MANDATORY water-distribution VIDEO's PROOF_UPLOAD item. Video-only since
     *  2026-08-11; a row queued earlier may reference a photo, which the backend now rejects. */
    @SerialName("water_proof_outbox_item_id") val waterProofOutboxItemId: String? = null,
    /**
     * SERVER proof ids for slots this phone did NOT shoot.
     *
     * A pen-session's three proofs may be recorded by three different operators (maintainer decision
     * 2026-08-14). A proof shot on another phone has no PROOF_UPLOAD outbox row here, so the outbox
     * ids above cannot name it — the dispatcher uses these instead, verbatim. The backend already
     * accepts them: ValidateFeedProofMedia checks tenant, upload state and media kind, never the
     * uploader.
     *
     * Nullable with a default so a row queued by an older build still decodes; a slot the operator
     * shot themselves leaves these null and resolves through its own outbox row exactly as before.
     */
    @SerialName("feed_weight_proof_ref") val feedWeightProofRef: String? = null,
    @SerialName("distribution_proof_ref") val distributionProofRef: String? = null,
    @SerialName("water_proof_ref") val waterProofRef: String? = null,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.WORKFLOW_ACTION_ANSWER].
 * Answers a question / question_select birth/death workflow action
 * (docs/decisions/birth-death-workflows.md). The WORKFLOW ID is the outbox group key so two
 * actions on the same workflow drain strictly oldest-first.
 */
@Serializable
data class WorkflowActionAnswerPayload(
    @SerialName("workflow_id") val workflowId: String,
    @SerialName("action_id") val actionId: String,
    @SerialName("answer_value") val answerValue: String,
    /** Outbox id of the mandatory video upload when the question requires_video. */
    @SerialName("proof_outbox_item_id") val proofOutboxItemId: String? = null,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.WORKFLOW_ACTION_COMPLETE].
 * Completes an `action`-type step. When the action `requires_video`, the MANDATORY video is carried
 * by REFERENCE to its PROOF_UPLOAD outbox row ([proofOutboxItemId]) — exactly like
 * [ShiftingCompletePayload]'s single mandatory video: the dispatcher resolves the row's uploaded
 * `proof_id` and sends it as `proof_ref`. The proof upload is enqueued on the SAME group (the
 * workflow id), so it drains first. Null for non-video completions.
 */
@Serializable
data class WorkflowActionCompletePayload(
    @SerialName("workflow_id") val workflowId: String,
    @SerialName("action_id") val actionId: String,
    @SerialName("proof_outbox_item_id") val proofOutboxItemId: String? = null,
)

@Serializable
data class HealthTreatmentCompletePayload(
    @SerialName("health_session_id") val healthSessionId: String,
    @SerialName("proof_ref") val proofRef: String = "",
)

@Serializable
data class HealthCaseOpenPayload(
    @SerialName("goat_id") val goatId: String,
    @SerialName("disease_key") val diseaseKey: String,
    @SerialName("age_band") val ageBand: String,
    @SerialName("start_date") val startDate: String,
    /** Local-only labels used while this write is waiting in the outbox. SyncEngine sends only
     * the canonical fields above to the backend. Defaults preserve already-queued payloads. */
    @SerialName("goat_display_id") val goatDisplayId: String = "",
    @SerialName("disease_name") val diseaseName: String = "",
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.FEED_PACKING_COMPLETE] —
 * the verifier-GATED packing flow. Simpler than [FeedDistributionCompletePayload]: only ONE
 * MANDATORY packing video, carried by reference to its PROOF_UPLOAD outbox row (exactly like
 * [ShiftingCompletePayload]'s single mandatory video). The dispatcher resolves the row's uploaded
 * `proof_id` and sends it as `packing_proof_ref`. The proof upload is enqueued on the SAME group as
 * this completion, so it drains first. The shed-session key is the outbox group key.
 */
@Serializable
data class FeedPackingCompletePayload(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String,
    /** The PEN worked; null for an undivided shed. Defaulted so an outbox row written by an older
     *  build still decodes — it simply predates per-pen completions. */
    @SerialName("partition_label") val partitionLabel: String? = null,
    /**
     * The feeding session this bag was packed for, and part of the completion's identity again
     * (maintainer decision 2026-08-11).
     *
     * DEFAULTED TO 0 ON PURPOSE, and 0 means "queued by the pen-day build". The outbox is durable, so
     * a phone upgrading across this change can still hold a recorded-but-unsynced packing row whose
     * JSON carries no session at all. A non-defaulted property would fail to decode it and kill an
     * operator's video terminally. [SyncEngine] maps a 0 to session 1 on dispatch, the same choice
     * migration 000150 makes for the rows already on the server.
     */
    @SerialName("session_no") val sessionNo: Int = 0,
    @SerialName("target_date") val targetDate: String,
    @SerialName("workflow") val workflow: String,
    /** Outbox id of the MANDATORY packing VIDEO's PROOF_UPLOAD item. */
    @SerialName("packing_proof_outbox_item_id") val packingProofOutboxItemId: String,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.FEED_WASTAGE_COMPLETE] —
 * the verifier-GATED leftover-feed flow on EXPERIMENT pens (maintainer decision 2026-08-18). Grain
 * is the PEN-DAY: no session (wastage is measured once per day) and no workflow (the server stamps
 * `experiment`). ONE MANDATORY video, carried by reference to its PROOF_UPLOAD outbox row exactly
 * like [FeedPackingCompletePayload]; the dispatcher resolves the uploaded `proof_id` and sends it
 * as `wastage_proof_ref`. The pen-day key is the outbox group key so the proof drains first.
 */
@Serializable
data class FeedWastageCompletePayload(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String,
    /** The PEN whose leftover was filmed; null for an undivided shed. Part of the completion's
     *  IDENTITY — a partitioned shed has one wastage task PER PEN. */
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("target_date") val targetDate: String,
    /** Outbox id of the MANDATORY wastage VIDEO's PROOF_UPLOAD item. */
    @SerialName("wastage_proof_outbox_item_id") val wastageProofOutboxItemId: String,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.FEED_WASTAGE_MEASUREMENT]:
 * the VERIFIER recording the leftover weight she reads off a wastage video (maintainer decision
 * 2026-08-18). The completion id comes from the item's backend-owned measurement_correction block
 * (`observation_id`, echoing source.ref_id) — this app never composes that address itself.
 */
@Serializable
data class FeedWastageMeasurementPayload(
    @SerialName("completion_id") val completionId: String,
    @SerialName("request") val request: FeedWastageMeasurementRequestDto,
)

@Serializable
data class MilkPreparationSubmitPayload(
    @SerialName("park_id") val parkId: String,
    @SerialName("preparation_date") val preparationDate: String,
    @SerialName("goat_milk_used") val goatMilkUsed: Boolean,
    @SerialName("answers") val answers: MilkPreparationAnswersPayload,
    /** Stable step-code -> PROOF_UPLOAD outbox row id. */
    @SerialName("proof_outbox_item_ids") val proofOutboxItemIds: Map<String, String>,
)

@Serializable
data class MilkPreparationAnswersPayload(
    val morningMilkCollectedLitres: Double,
    val eveningMilkCollectedLitres: Double,
    val goatMilkQuantityLitres: Double,
    val boilingTemperatureC: Double,
    val cooledTemperatureC: Double,
    val uhtMilkQuantityLitres: Double,
    val citricAcidGrams: Double,
)

@Serializable
data class MilkFeedingSubmitPayload(
    val taskId: String,
    val parkId: String,
    val feedingDate: String,
    val sessionNo: Int,
    val answers: MilkFeedingAnswersDto,
    val cleanBottlesProofOutboxItemId: String,
    val mixingAndFillingProofOutboxItemId: String,
)

@Serializable data class FeedTransportSubmitPayload(@SerialName("task_id") val taskId:String,@SerialName("proof_outbox_item_id") val proofOutboxItemId:String)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.CLOCK_IN] /
 * [sg.mesha.goatos.core.database.outbox.OutboxOpType.CLOCK_OUT] (module clock, maintainer
 * decision 2026-08-27). The request carries the whole punch capture — device-clock tap time,
 * location, integrity verdict, battery, network kind, offline flag — frozen at TAP time, so a
 * drain hours later still reports what the device honestly knew when the person punched. The
 * body's own `idempotency_key` equals the outbox row's stable day-scoped key.
 */
@Serializable
data class ClockPunchPayload(
    @SerialName("request") val request: ClockPunchRequestDto,
)
