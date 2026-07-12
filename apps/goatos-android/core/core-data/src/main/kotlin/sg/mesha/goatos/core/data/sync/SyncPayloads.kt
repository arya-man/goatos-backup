package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto

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
