package sg.mesha.goatos.viewmodel

/**
 * The identity of ONE feed capture flow: an exact shed, on one feed DAY, in one session, in one
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
 * [partitionLabel] is the raw legacy partition from the row ("2", "Part 3"), blank for an
 * undivided/exact shed.
 *
 * Exact shed id is the physical shed identity. [partitionLabel] remains in the function signature
 * for older callers, but it is compatibility metadata and must not split one exact shed's drafts.
 * [locationIdentityKey] is a temporary hidden escape hatch for stale cached/pre-cutover rows whose
 * `shedId` is still the old parent. Exact rows pass blank or the same value as [shedId].
 */
internal fun feedCaptureGroupKey(
    prefix: String,
    shedId: String,
    partitionLabel: String,
    sessionNo: Int,
    workflow: String,
    targetDate: String,
    locationIdentityKey: String = "",
): String =
    "$prefix:${dateToken(targetDate)}:${locationIdentityKey.trim().ifEmpty { shedId }}:$sessionNo:$workflow"

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

private const val UNDATED_TOKEN = "undated"
