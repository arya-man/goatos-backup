package sg.mesha.goatos.viewmodel

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
internal fun feedCaptureGroupKey(
    prefix: String,
    shedId: String,
    partitionLabel: String,
    sessionNo: Int,
    workflow: String,
    targetDate: String,
): String =
    "$prefix:${dateToken(targetDate)}:$shedId:${partitionMatchToken(partitionLabel)}:$sessionNo:$workflow"

/**
 * The identity of one feed PACKING capture: a shed's PEN, on one feed DAY, in one workflow.
 *
 * No session (maintainer decision 2026-08-10). One video covers the pen's whole day, so all three
 * things this key drives -- the durable draft, the submit idempotency key, and the outbox group --
 * are pen-day scoped, matching the completion's natural key on the server.
 *
 * A dedicated function rather than calling [feedCaptureGroupKey] with `sessionNo = 0`: a literal
 * zero in a call site reads as a session number and invites the next author to pass a real one,
 * which would split one pen-day into two drafts and two idempotency keys -- letting the same pen be
 * filmed and submitted twice. [PEN_DAY_TOKEN] states that the segment is not a session at all.
 * DISTRIBUTION is still per shed-session and keeps [feedCaptureGroupKey] unchanged.
 *
 * Upgrade note: this key differs from the one an earlier build used, so a capture left half-recorded
 * across the upgrade is not rehydrated and the operator re-films. Nothing is lost that was already
 * submitted -- an ALREADY-QUEUED outbox row carries its own stored group and idempotency keys and
 * still drains untouched.
 */
internal fun feedPackingCaptureGroupKey(
    shedId: String,
    partitionLabel: String,
    workflow: String,
    targetDate: String,
): String =
    "feed-pack:${dateToken(targetDate)}:$shedId:${partitionMatchToken(partitionLabel)}:$PEN_DAY_TOKEN:$workflow"

private const val PEN_DAY_TOKEN = "day"

/**
 * The feed day as a key segment.
 *
 * Blank collapses to [UNDATED_TOKEN] rather than an empty segment, so a route that somehow omits the
 * date still yields a well-formed key instead of one that reads `feed-pack::shed-x:...`. That is a
 * degraded case, not a supported one — two undated opens on different days DO still share a key —
 * but every real caller passes the row's `target_date`, and a missing one is a routing bug to fix at
 * the route rather than something to paper over with a device clock read here. Reading the clock
 * would be worse: it would key an in-progress capture to the day it happened to be opened, so a
 * capture started before midnight would lose its draft when submitted after.
 */
private fun dateToken(targetDate: String): String = targetDate.trim().ifEmpty { UNDATED_TOKEN }

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

private const val UNDATED_TOKEN = "undated"

private val WHITESPACE_RUN = Regex("\\s+")
