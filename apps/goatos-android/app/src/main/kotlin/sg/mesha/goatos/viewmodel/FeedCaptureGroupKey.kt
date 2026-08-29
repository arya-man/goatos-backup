package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofFlow

/**
 * The identity of ONE feed capture flow: a shed's PEN, on one feed DAY, in one session, in one
 * workflow.
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
 * [targetDate] is the SAME defect one dimension over, and it predates the pen fix rather than
 * arriving with it: the key has never carried the feed day, so today's capture for a pen/session it
 * already ran yesterday rehydrates YESTERDAY's committed draft — success screen, no filming
 * possible — and its completion collapses on the backend as a replay of yesterday's. A feed task is
 * created per shed per DAY, so the day is part of the task's identity and belongs in the key that
 * claims to be that task's identity. Pass the row's own `target_date` (`YYYY-MM-DD`); it is already
 * on the route and already sent in the request body.
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
/**
 * Constructs the ProofIdentity for a feed capture flow. Used internally to build the
 * captureGroupKey and externally by ViewModels to build EvidenceSlot for captureReplacingLatest().
 */
internal fun buildFeedProofIdentity(
    prefix: String,
    shedId: String,
    partitionLabel: String,
    sessionNo: Int,
    workflow: String,
    targetDate: String,
): ProofIdentity {
    // Map prefix to ProofFlow
    val proofFlow = when (prefix) {
        "feed-pack" -> ProofFlow.FEED_PACKING
        "feed-dist" -> ProofFlow.FEED_DISTRIBUTION
        "feed-transport" -> ProofFlow.FEED_TRANSPORT
        "feed-wastage" -> ProofFlow.FEED_WASTAGE
        else -> ProofFlow.FEED_COMPLETE
    }

    return ProofIdentity(
        flow = proofFlow,
        taskId = shedId,
        partitionKey = partitionMatchToken(partitionLabel),
        flowPrefix = prefix,
        targetDate = targetDate,
        sessionNo = sessionNo,
        workflow = workflow,
    )
}

internal fun feedCaptureGroupKey(
    prefix: String,
    shedId: String,
    partitionLabel: String,
    sessionNo: Int,
    workflow: String,
    targetDate: String,
): String {
    val identity = buildFeedProofIdentity(prefix, shedId, partitionLabel, sessionNo, workflow, targetDate)
    return identity.captureGroupKey()
}

/**
 * Storage-addressing identity for feed evidence slots: capture()/observeProofs() address feed
 * proofs by the FULL capture group key as taskId, with partitionKey "whole" — the pen identity
 * is already embedded in the group key (partitionMatchToken segment). Writing with a pen-scoped
 * partitionKey while reading group-scoped is exactly the write/read divergence Manohar's
 * fb3c74af3 fixed; this builder exists so slot construction cannot reintroduce it.
 */
internal fun buildFeedEvidenceSlotIdentity(
    prefix: String,
    shedId: String,
    partitionLabel: String,
    sessionNo: Int,
    workflow: String,
    targetDate: String,
): ProofIdentity {
    val identity = buildFeedProofIdentity(prefix, shedId, partitionLabel, sessionNo, workflow, targetDate)
    return identity.copy(taskId = identity.captureGroupKey(), partitionKey = "whole")
}

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
