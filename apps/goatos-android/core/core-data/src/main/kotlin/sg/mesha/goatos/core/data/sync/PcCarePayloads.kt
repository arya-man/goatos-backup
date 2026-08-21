package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Outbox payloads and the ONE definition of every PC Care outbox key (module pc_care, maintainer
 * decision 2026-08-21). Every idempotency key and group key a PC Care write uses is built HERE and
 * nowhere else — a second call site rebuilding a key is exactly how the feed "In review" badge
 * broke three separate ways (see SubmittedGrainKeys.kt's header).
 */

/** Group key for a task's PROOF_UPLOAD rows, slot registrations, and the final submit — one FIFO
 *  lane per task, so every proof upload drains strictly BEFORE the slot registration that resolves
 *  it and before the submit that needs the slots complete. The capture screen must pass this SAME
 *  value as its `uploadGroupKey` when enqueueing PC Care proof videos. */
fun pcCareTaskGroupKey(taskId: String): String = "pc-care:task:$taskId"

/** Group key for a task's scan writes. Deliberately SEPARATE from [pcCareTaskGroupKey]: a scan
 *  carries no video and must not queue behind a multi-megabyte proof upload — a peer's phone
 *  learns about the scanned animal through the captures poll, so scan latency is user-visible. */
fun pcCareScanGroupKey(taskId: String): String = "pc-care:scan:$taskId"

/** STABLE per (task, normalized tag): a retry replays for free; the SAME tag scanned again maps to
 *  the same key, so a double-tap can never create two server rows. Never timestamp-suffixed. */
fun pcCareScanIdempotencyKey(taskId: String, normalizedTag: String): String =
    "pc-care:scan:$taskId:$normalizedTag"

/** STABLE per (task, tag, slot, proof row): re-attaching the SAME clip replays for free, while a
 *  re-shoot (a new PROOF_UPLOAD outbox row) is a different act under a different key. */
fun pcCareSlotIdempotencyKey(
    taskId: String,
    normalizedTag: String,
    slotFieldKey: String,
    proofOutboxItemId: String,
): String = "pc-care:slot:$taskId:$normalizedTag:$slotFieldKey:$proofOutboxItemId"

/** STABLE per (task, row version): a retry replays for free, while a submit after a verifier
 *  rework (which bumps row_version) is a genuinely new act under a new key. */
fun pcCareSubmitIdempotencyKey(taskId: String, rowVersion: Int): String =
    "pc-care:submit:$taskId:rv:$rowVersion"

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.PC_CARE_SCAN_ADD] —
 * one RFID scanned into a task, VERBATIM ([tagVerbatim] is what the server stores; [normalizedTag]
 * is the local duplicate-check key and part of the idempotency key).
 */
@Serializable
data class PcCareScanAddPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("tag_verbatim") val tagVerbatim: String,
    @SerialName("normalized_tag") val normalizedTag: String,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.PC_CARE_SLOT_REGISTER] —
 * attaches ONE slot's live-camera video to ONE scanned animal
 * (`PUT /app/pc-care/tasks/{task}/animals/{row}/proofs/{slot}`). The video rides by REFERENCE to
 * its PROOF_UPLOAD outbox row ([proofOutboxItemId]) on the same group, so it drains first and the
 * dispatcher resolves the uploaded server proof id. [animalRowId] may be BLANK at enqueue time —
 * the scan may not have synced yet — and is re-resolved from the Room animal row at dispatch.
 */
@Serializable
data class PcCareSlotRegisterPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("animal_row_id") val animalRowId: String = "",
    @SerialName("normalized_tag") val normalizedTag: String,
    @SerialName("slot_field_key") val slotFieldKey: String,
    @SerialName("proof_outbox_item_id") val proofOutboxItemId: String,
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.PC_CARE_TASK_SUBMIT] —
 * submits the WHOLE task (`POST /app/pc-care/tasks/{task}/submit`). [rowVersion] is the task's
 * optimistic-concurrency token the screen last rendered; it is part of the idempotency key so a
 * post-rework re-submit is a new act, never a replay of the pre-rework one.
 */
@Serializable
data class PcCareTaskSubmitPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("row_version") val rowVersion: Int,
)
