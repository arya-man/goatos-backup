package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Outbox payload and the ONE definition of every pen-visit outbox key (maintainer decision
 * 2026-09-07). Every idempotency key and group key a pen-visit write uses is built HERE and
 * nowhere else — a second call site rebuilding a key is exactly how the feed "In review" badge
 * broke three separate ways (see SubmittedGrainKeys.kt's header).
 */

/** Group key for a visit's PROOF_UPLOAD row and its submit — one FIFO lane per visit, so the
 *  upload drains strictly BEFORE the submit that references it. The capture path must pass this
 *  SAME value as its `uploadGroupKey` when enqueueing the visit video. */
fun penVisitTaskGroupKey(taskId: String): String = "pen-visit:task:$taskId"

/** STABLE per (task, row version): a retry replays for free, while a submit after the task moved
 *  (bumped row_version) is a genuinely new act under a new key. Never timestamp-suffixed. */
fun penVisitSubmitIdempotencyKey(taskId: String, rowVersion: Int): String =
    "pen-visit:submit:$taskId:$rowVersion"

/** The capture identity's field key: ONE video per visit, replaced on a re-record. */
const val PEN_VISIT_VIDEO_FIELD_KEY = "visit-video"

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.PEN_VISIT_SUBMIT] —
 * the visit's one video (`POST /app/pen-visits/{task}/submit`). The video rides by REFERENCE to
 * its PROOF_UPLOAD outbox row ([proofOutboxItemId]) on the same task group, so the upload drains
 * first and the dispatcher resolves the uploaded server proof id. [rowVersion] is the task's
 * optimistic-concurrency token the screen last rendered.
 */
@Serializable
data class PenVisitSubmitPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("row_version") val rowVersion: Int,
    @SerialName("proof_outbox_item_id") val proofOutboxItemId: String,
)
