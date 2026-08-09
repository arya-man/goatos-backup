package sg.mesha.goatos.feature.feed

/**
 * Whether a feed shed-session row may be OPENED for capture.
 *
 * Two independent gates, and the row needs BOTH:
 *  - [isToday] — the screen-level "this is the current packing/feed day" flag. A past day is view
 *    only.
 *  - the row's own backend-owned lifecycle bucket — a session already awaiting a verdict, or already
 *    approved, is not the operator's to act on.
 *
 * The second gate was missing (reported on STG 2026-08-09). `lifecycleStatus` was rendered as a chip
 * and never consulted, so a session showing "in review" stayed fully tappable; the detail screen
 * never receives the status and decides "already recorded?" from the LOCAL draft alone, so on a
 * fresh install — where no draft exists — it offered an empty capture form for work already
 * submitted. The operator re-shot a video the backend then discarded as an idempotent replay.
 *
 * REWORK STAYS CAPTURABLE, and that is why this keys on "is pending" rather than on a list of
 * blocked states. The raw completion table has a fourth state, 'rework' (verifier rejected), which
 * `domain.NormalizeSessionStatus` deliberately merges into [FeedStatus.PENDING] before it reaches a
 * client: from the operator's list a bounced proof is "needs my action again", the same bucket as
 * never-submitted. A rejected session that could not be reopened would strand every re-shoot, so the
 * pinning test for that case guards this file and the backend mapping together.
 *
 * An empty/unknown status is treated as pending — the same fail-toward-the-operator reading
 * NormalizeSessionStatus applies, and it keeps a row usable if the field is ever absent rather than
 * silently locking work nobody can reach.
 */
fun feedSessionCanCapture(lifecycleStatus: String, isToday: Boolean): Boolean {
    if (!isToday) return false
    return when (lifecycleStatus.trim()) {
        FeedStatus.AWAITING, FeedStatus.COMPLETED -> false
        else -> true
    }
}
