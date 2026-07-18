package sg.mesha.goatos.push

import sg.mesha.goatos.ui.Routes
import sg.mesha.goatos.ui.calendarTargetRoute

/** `type`/`screen` values that mean "open the read-only/verify record" for the push's shed. */
private val RECORD_TYPES = setOf("record", "verification", "verification_closed", "verify", "rework")

/**
 * Maps an FCM data payload ([PushExtras]) to an app route.
 *
 * Reuses [calendarTargetRoute] — the SAME backend-href -> route mapping Calendar taps already
 * use — for any payload carrying an explicit `target`/`href`, so a push opens exactly where a
 * Calendar tap on the same backend item would. Falling back to `screen`/`type` + `shed_id`/
 * `obligation_id` covers a push that has no pre-computed href (e.g. a raw reminder/verify
 * signal the backend fired directly, not routed through a Calendar item):
 *  - `screen`/`type` "record"/"verification"/"verify"/"rework" -> the read-only/verify record
 *    for [PushExtras.SHED_ID] (falls back to the Vaccination landing with no shed id).
 *  - `screen`/`type` "reschedule" -> the reschedule form for [PushExtras.OBLIGATION_ID].
 *  - `type` "reminder" or `screen` "scan"/"shed" -> the execute (scan) loop for the shed — a
 *    reminder opens the drive/shed, not a passive summary.
 *  - `screen` "calendar" -> Calendar.
 *  - anything else / no recognizable field -> the Vaccination landing (never a crash or blank
 *    screen for an unrecognized push shape — see [sg.mesha.goatos.MainActivity]'s "robust
 *    static graph" note on [Routes]).
 */
fun resolvePushRoute(payload: Map<String, String>): String {
    val target = payload[PushExtras.TARGET]?.takeIf { it.isNotBlank() }
        ?: payload[PushExtras.HREF]?.takeIf { it.isNotBlank() }
    if (target != null) return calendarTargetRoute(target)

    val shedId = payload[PushExtras.SHED_ID]?.takeIf { it.isNotBlank() }
    val obligationId = payload[PushExtras.OBLIGATION_ID]?.takeIf { it.isNotBlank() }
    val itemId = payload[PushExtras.ITEM_ID]?.takeIf { it.isNotBlank() }
    val category = payload[PushExtras.CATEGORY]?.takeIf { it.isNotBlank() }
    val screen = payload[PushExtras.SCREEN]?.lowercase()?.takeIf { it.isNotBlank() }
    val type = payload[PushExtras.TYPE]?.lowercase()?.takeIf { it.isNotBlank() }

    return when {
        screen == "verification" || type == "verification_pending" ->
            if (itemId != null) Routes.verifyDetailRoute(itemId, category) else Routes.VERIFY
        screen == "leadership_close" || type == "verification_approved" -> Routes.LEADERSHIP
        screen in RECORD_TYPES || type in RECORD_TYPES ->
            if (shedId != null) Routes.recordRoute(shedId) else Routes.VACCINATION
        screen == "reschedule" || type == "reschedule" -> Routes.rescheduleRoute(obligationId)
        screen == "scan" || screen == "shed" || type == "reminder" ->
            if (shedId != null) Routes.scanRoute(shedId) else Routes.VACCINATION
        screen == "calendar" || type == "calendar" -> Routes.CALENDAR
        else -> Routes.VACCINATION
    }
}
