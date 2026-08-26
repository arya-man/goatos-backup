package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Outbox payloads and the ONE definition of every Toxin outbox key (module toxin, maintainer
 * decision 2026-08-25). Every idempotency key and group key a Toxin write uses is built HERE and
 * nowhere else — a second call site rebuilding a key is exactly how the feed "In review" badge
 * broke three separate ways (see SubmittedGrainKeys.kt's header).
 */

/** Group key for a task's PROOF_UPLOAD rows, step completions, and the final reading submit —
 *  one FIFO lane per test task, so every proof upload drains strictly BEFORE the step completion
 *  (or submit) that resolves it, and two steps of the SAME task can never race out of order. The
 *  capture path must pass this SAME value as its `uploadGroupKey` when enqueueing toxin proofs. */
fun toxinTaskGroupKey(taskId: String): String = "toxin:task:$taskId"

/** STABLE per (task, step, proof row): a retry replays for free, while a re-shoot (a new
 *  PROOF_UPLOAD outbox row) is a genuinely different act under a different key. Never
 *  timestamp-suffixed. */
fun toxinStepIdempotencyKey(taskId: String, stepNo: Int, proofOutboxItemId: String): String =
    "toxin:step:$taskId:$stepNo:$proofOutboxItemId"

/** STABLE per (task, outcome, strip-photo row): a retry replays for free, while a changed
 *  reading or a re-shot strip photo is a new act under a new key. */
fun toxinSubmitIdempotencyKey(taskId: String, outcome: String, stripPhotoOutboxItemId: String): String =
    "toxin:submit:$taskId:$outcome:$stripPhotoOutboxItemId"

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.TOXIN_STEP_COMPLETE] —
 * one guided step's completion (`POST /app/toxin/tasks/{task}/steps/{step}/complete`). The proof
 * rides by REFERENCE to its PROOF_UPLOAD outbox row ([proofOutboxItemId]) on the same task group,
 * so the upload drains first and the dispatcher resolves the uploaded server proof id.
 */
@Serializable
data class ToxinStepCompletePayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("step_no") val stepNo: Int,
    @SerialName("proof_outbox_item_id") val proofOutboxItemId: String,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.TOXIN_SUBMIT] — step 7's
 * strip reading (`POST /app/toxin/tasks/{task}/submit`). [outcome] is one of the BACKEND-OWNED
 * `outcome_options` values the detail contract served; the strip PHOTO rides by reference to its
 * PROOF_UPLOAD row exactly like a step video.
 */
@Serializable
data class ToxinSubmitPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("outcome") val outcome: String,
    @SerialName("strip_photo_outbox_item_id") val stripPhotoOutboxItemId: String,
)
