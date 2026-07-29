package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationCloseRequestDto

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
 * dispatcher resolves each row's uploaded `proof_id` and sends the pair as `distribution_proof_ref`
 * / `water_proof_ref`. Both proof uploads are enqueued on the SAME group as this completion, so they
 * drain first. The shed-session key is the outbox group key.
 */
@Serializable
data class FeedDistributionCompletePayload(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String,
    @SerialName("session_no") val sessionNo: Int,
    @SerialName("target_date") val targetDate: String,
    @SerialName("workflow") val workflow: String,
    /** Outbox id of the MANDATORY feed-distribution VIDEO's PROOF_UPLOAD item. */
    @SerialName("distribution_proof_outbox_item_id") val distributionProofOutboxItemId: String,
    /** Outbox id of the MANDATORY water-distribution proof's (photo or video) PROOF_UPLOAD item. */
    @SerialName("water_proof_outbox_item_id") val waterProofOutboxItemId: String,
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
    @SerialName("session_no") val sessionNo: Int,
    @SerialName("target_date") val targetDate: String,
    @SerialName("workflow") val workflow: String,
    /** Outbox id of the MANDATORY packing VIDEO's PROOF_UPLOAD item. */
    @SerialName("packing_proof_outbox_item_id") val packingProofOutboxItemId: String,
)

@Serializable
data class MilkPreparationSubmitPayload(
    @SerialName("park_id") val parkId: String,
    @SerialName("preparation_date") val preparationDate: String,
    @SerialName("goat_milk_used") val goatMilkUsed: Boolean,
    /** Stable step-code -> PROOF_UPLOAD outbox row id. */
    @SerialName("proof_outbox_item_ids") val proofOutboxItemIds: Map<String, String>,
)

@Serializable data class FeedTransportSubmitPayload(@SerialName("task_id") val taskId:String,@SerialName("proof_outbox_item_id") val proofOutboxItemId:String)
