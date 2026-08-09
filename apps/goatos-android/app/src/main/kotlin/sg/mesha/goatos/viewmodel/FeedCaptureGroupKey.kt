package sg.mesha.goatos.viewmodel

/**
 * The identity of ONE feed capture flow: a shed's PEN, in one session, in one workflow.
 *
 * This key is load-bearing three times over, which is why it lives in one tested function instead
 * of being re-typed in each view model:
 *  - the DURABLE capture draft ([CaptureDraftRepository.find]) — what has already been recorded;
 *  - the completion's submit idempotency key — what the backend collapses as a replay;
 *  - the outbox group key — what orders the proof upload before the completion that references it.
 *
 * The pen was missing from all three until 2026-08-09, so every pen of a shed-session shared one
 * draft and one idempotency key. Reported from the field: after filming one pen, opening a SIBLING
 * pen showed "video uploaded successfully" for a clip nobody had recorded there, and that pen could
 * not be filmed at all (the shared draft also carries the committed flag). Worse and quieter, the
 * shared idempotency key meant the sibling pen's submission was collapsed as a replay of the first
 * pen's — no completion row, no verification item, no video, and a success screen on the phone.
 *
 * This is the phone-side tail of the 2026-08-08 pen-collapse defect. That round fixed the natural
 * key (migration 000137), the packing line grain (`BuildPackingRows`), the read overlay
 * (`app.completedKey`) and the client's list `grainKey` — but the capture draft was keyed here, one
 * hop further on, and was missed.
 *
 * [partitionLabel] is the raw pen from the row ("2", "Part 3"), blank for an undivided shed.
 *
 * NOTE ON NORMALIZATION: [partitionMatchToken] is a DEVICE-LOCAL token, not the backend's
 * `domain.PartitionMatchKey`. It never leaves the phone — the request body carries the raw
 * `partition_label`, and the server does its own normalizing. It exists only so a whitespace or
 * case variant of the same pen across two page loads cannot split one pen into two drafts. Do not
 * "align" it with the Go function: that would create a twin needing to be kept in sync for no
 * behaviour, which is the drift this codebase keeps paying for.
 */
internal fun feedCaptureGroupKey(
    prefix: String,
    shedId: String,
    partitionLabel: String,
    sessionNo: Int,
    workflow: String,
): String = "$prefix:$shedId:${partitionMatchToken(partitionLabel)}:$sessionNo:$workflow"

/**
 * Device-local matching token for a pen: trimmed, lowercased, internal whitespace collapsed.
 *
 * Blank collapses to [WHOLE_SHED_TOKEN] so an undivided shed has ONE stable key rather than an
 * empty segment — the same "a shed with no pens is still exactly one operational location" rule the
 * generated `partition_key` column encodes. Never render it: "whole" is a key, never copy.
 */
internal fun partitionMatchToken(partitionLabel: String): String {
    val normalized = partitionLabel.trim().lowercase().replace(WHITESPACE_RUN, " ")
    return normalized.ifEmpty { WHOLE_SHED_TOKEN }
}

private const val WHOLE_SHED_TOKEN = "whole"

private val WHITESPACE_RUN = Regex("\\s+")
