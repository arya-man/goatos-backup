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
 *  - reminder/shed payloads -> Vaccination. Push taps never open Scan; scanning starts only after
 *    the operator picks a shed from Vaccination.
 *  - `screen` "calendar" -> Calendar.
 *  - anything else / no recognizable field -> the Vaccination landing (never a crash or blank
 *    screen for an unrecognized push shape — see [sg.mesha.goatos.MainActivity]'s "robust
 *    static graph" note on [Routes]).
 */
fun resolvePushRoute(payload: Map<String, String>): String {
    val shedId = payload[PushExtras.SHED_ID]?.takeIf { it.isNotBlank() }
    val itemId = payload[PushExtras.ITEM_ID]?.takeIf { it.isNotBlank() }
    val category = payload[PushExtras.CATEGORY]?.takeIf { it.isNotBlank() }
    val role = payload[PushExtras.ROLE]?.lowercase()?.takeIf { it.isNotBlank() }
    val screen = payload[PushExtras.SCREEN]?.lowercase()?.takeIf { it.isNotBlank() }
    val type = payload[PushExtras.TYPE]?.lowercase()?.takeIf { it.isNotBlank() }

    if (screen == "scan" || screen == "shed" || screen == "vaccination" || type == "reminder" || type == "vaccination_reminder") {
        return Routes.VACCINATION
    }

    val target = payload[PushExtras.TARGET]?.takeIf { it.isNotBlank() }
        ?: payload[PushExtras.HREF]?.takeIf { it.isNotBlank() }
    if (target != null) return calendarTargetRoute(target)

    return when {
        screen == "verification" || type == "verification_pending" ->
            when {
                role == "verifier" -> if (itemId != null) Routes.verifyDetailRoute(itemId, category) else Routes.VERIFY
                else -> Routes.VACCINATION
            }
        screen == "leadership_close" || type == "verification_approved" -> Routes.VACCINATION
        screen in RECORD_TYPES || type in RECORD_TYPES ->
            if (shedId != null) Routes.recordRoute(shedId) else Routes.VACCINATION
        screen == "reschedule" || type == "reschedule" -> Routes.VACCINATION
        screen == "calendar" || type == "calendar" -> Routes.CALENDAR
        else -> Routes.VACCINATION
    }
}
