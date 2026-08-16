package sg.mesha.goatos.viewmodel

/**
 * The verification-lifecycle a Feed list row RENDERS, given the backend row and this phone's
 * optimistic "already submitted" hint.
 *
 * A queued-but-unsynced submit still reads "pending" from the backend page (the write is sitting in
 * the outbox), so without this the operator returns to the list and sees "Pending" for work they
 * just submitted — the bug in 254.mp4. Feed Packing, Feed Direction and Feed Distribution all render
 * their chip from `lifecycleStatus`, so they ALL go through this one rule; a second copy is how the
 * flows drifted apart in the first place.
 *
 * Precedence, in order:
 *   a. backend "completed" -> keep it; server acceptance always overrides a local hint.
 *   b. non-blank [reworkReason] -> keep the backend status. A reworked line comes back as "pending"
 *      WITH a reason, so overlaying it would hide the rejection the operator must act on. Feeds
 *      without a rework channel pass "" and skip this rung.
 *   c. locally submitted for review -> "pending_verification".
 *   d. otherwise -> the backend status.
 *
 * Top-level and pure on purpose: the list projections and their tests call THIS, so the rule has a
 * single definition and a test cannot pass against a copy of the logic.
 */
internal fun overlayFeedLifecycleStatus(
    lifecycleStatus: String,
    reworkReason: String,
    isLocallySubmittedForReview: Boolean,
): String = when {
    lifecycleStatus == "completed" -> "completed"
    reworkReason.isNotBlank() -> lifecycleStatus
    isLocallySubmittedForReview -> "pending_verification"
    else -> lifecycleStatus
}
