package sg.mesha.goatos.push

import sg.mesha.goatos.ui.Routes
import sg.mesha.goatos.ui.pushTargetRoute

/** `type`/`screen` values that mean "open the read-only/verify record" for the push's shed. */
private val RECORD_TYPES = setOf("record", "verification_closed", "rework")

/**
 * Maps an FCM data payload ([PushExtras]) to an app route, or null when the payload names no
 * destination this build can open.
 *
 * Resolution runs MOST SPECIFIC FIRST, and that ordering is the contract:
 *  1. the explicit `target`/`href` the backend computed for THIS recipient — the verifier's
 *     `/verification/items/{id}` video review, or a module landing such as `/weighing`;
 *  2. the named `screen`/`type`, for a signal the backend fired without a pre-computed link;
 *  3. nothing — the caller lands the person on their OWN home screen.
 *
 * An earlier version tested `screen` first and then fell back to Vaccination for anything it did
 * not recognise. Both halves were wrong: the screen short-circuit ran before the target was ever
 * read, so a verifier's tap could not reach the video it was sent for, and the blind fallback
 * dropped a weighing-only or feed-only person onto Vaccination.
 *
 * The recipient's ROLE is deliberately not consulted. Who someone is comes from their own sign-in,
 * never from a field on a message a sender could fill in wrong.
 */
fun resolvePushRoute(payload: Map<String, String>): String? {
    val shedId = payload[PushExtras.SHED_ID]?.takeIf { it.isNotBlank() }
    val itemId = payload[PushExtras.ITEM_ID]?.takeIf { it.isNotBlank() }
    val category = payload[PushExtras.CATEGORY]?.takeIf { it.isNotBlank() }
    val screen = payload[PushExtras.SCREEN]?.lowercase()?.takeIf { it.isNotBlank() }
    val type = payload[PushExtras.TYPE]?.lowercase()?.takeIf { it.isNotBlank() }

    val target = payload[PushExtras.TARGET]?.takeIf { it.isNotBlank() }
        ?: payload[PushExtras.HREF]?.takeIf { it.isNotBlank() }
    pushTargetRoute(target)?.let { return it }

    return when {
        // A NAMED module screen is read before the proof-review shapes below, because the same
        // waiting-proof event is sent twice with the same `type`: once to the verifier, who is
        // given the item to review, and once to that module's director, who is given the module.
        // Reading the type first sent the Feed Director into the verifier's queue.
        // Each module names its own screen; there is no shared default, so a module with no named
        // screen lands the person on their own home screen instead of on another module's.
        screen == "scan" || screen == "shed" || screen == "vaccination" ||
            screen == "vaccination_overview" || screen == "reschedule" ||
            type == "reminder" || type == "vaccination_reminder" || type == "reschedule" ->
            Routes.VACCINATION
        // The weighing consumers are not consistent about which screen they name: the submission
        // consumer says "weighing_overview" while the lifecycle consumer says "weighing" for
        // published, rework, closed and shed-reopened. Accept both, and match those four types
        // directly, because a weighing push that matches nothing here falls through to the
        // principal's landing route -- which for anyone who also holds vaccination is the
        // Vaccination screen, i.e. the exact defect this resolver exists to prevent.
        screen == "weighing_overview" || screen == "weighing" ||
            type == "weighing_campaign_published" || type == "weighing_rework" ||
            type == "weighing_campaign_closed" || type == "weighing_shed_reopened" ->
            Routes.WEIGHING
        screen == "feed_overview" -> Routes.FEED_DIRECTION
        // This tree has no /counts census root: Counts is reached through its module-scoped
        // sub-routes, and /counts/birth is the landing href the backend registry serves.
        screen == "counts_overview" -> Routes.COUNTS_BIRTH
        screen == "calendar" || type == "calendar" -> Routes.CALENDAR
        // Waiting proof video. Ahead of the record shapes below because a verifier's job is the
        // review itself, not the shed's record.
        screen == "verification" || screen == "verify" || type == "verification_pending" ->
            if (itemId != null) Routes.verifyDetailRoute(itemId, category) else Routes.VERIFY
        // RECORD_TYPES is module-blind: Routes.recordRoute is the VACCINATION record. A generic
        // rework notice carries screen="record" for every module, so without the category gate a
        // weighing rework opens the vaccination record for that shed.
        (screen in RECORD_TYPES || type in RECORD_TYPES) && isVaccinationCategory(category) ->
            shedId?.let { Routes.recordRoute(it) }
        else -> null
    }
}

/**
 * Whether a push's category belongs to vaccination.
 *
 * The record route is vaccination's, so a module-blind match on screen="record" sends a weighing
 * or feed rework into the vaccination record for that shed. Backend categories are
 * vaccination-prefixed ("vaccination", "vaccination_proof"), so a prefix test covers today's
 * values and any sibling added later. A push with no category is NOT assumed to be vaccination:
 * the safe miss is the person's own home screen, never another module's record.
 */
private fun isVaccinationCategory(category: String?): Boolean =
    category?.startsWith("vaccination") == true
