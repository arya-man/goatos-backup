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
 * The "Mark done" that RELOCATES the animals. The SHIFTING EVENT ID is the outbox group key, so two
 * actions on the same movement can never drain concurrently or out of order. [destinationTag] is
 * normally null — the server derives the destination cohort from the destination shed's profile; it
 * is only sent when completing into an empty shed.
 *
 * The OPTIONAL video is deliberately NOT carried here. It is captured and uploaded through the
 * existing [sg.mesha.goatos.core.database.outbox.OutboxOpType.PROOF_UPLOAD] op — registered against
 * the destination shed with `shifting_event_id` in its metadata — so it flows to GCS via the same
 * signed-URL path as vaccination proof, independently of this completion. Completion is not gated on
 * it (video is optional for now), and coupling the two would make a movement in a dead-signal shed
 * un-completable until its video finished uploading.
 */
@Serializable
data class ShiftingCompletePayload(
    @SerialName("shifting_event_id") val shiftingEventId: String,
    @SerialName("destination_tag") val destinationTag: String? = null,
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
