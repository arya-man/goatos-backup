package sg.mesha.goatos.core.analytics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

/**
 * Guards the [AnalyticsEventsSession] taxonomy the same way [AnalyticsContractTest] guards
 * [AnalyticsEvents]: names are a contract, pinned here so a rename is a conscious, reviewed act
 * rather than a silent funnel break.
 *
 * Also re-asserts the Firebase RESERVED event-name lesson from
 * [AnalyticsEvents.SESSION_START]'s KDoc against every name in THIS object — a second
 * session/navigation event surface is exactly where a reserved collision (`session_start`,
 * `first_open`, `app_remove`, `screen_view`, …) could get reintroduced by someone who never saw
 * the original incident.
 */
class AnalyticsEventsSessionContractTest {

    /** Firebase's documented automatically-collected / reserved event names — see
     *  https://support.google.com/firebase/answer/9234069. Any exact match here is silently
     *  dropped by Firebase, exactly like the incident that renamed `session_start` to
     *  `app_session_start` (see [AnalyticsEvents.SESSION_START]). */
    private val firebaseReservedEventNames = setOf(
        "app_clear_data", "app_remove", "app_uninstall", "app_update",
        "error", "first_open", "first_visit", "in_app_purchase",
        "notification_dismiss", "notification_foreground", "notification_open",
        "notification_receive", "os_update", "session_start", "session_start_with_rollout",
        "user_engagement", "ad_impression", "ad_click", "ad_query", "adunit_exposure",
        "screen_view",
    )

    @Test
    fun `event names are the exact snake_case contract`() {
        assertEquals("app_backgrounded", AnalyticsEventsSession.APP_BACKGROUNDED)
        assertEquals("session_restored", AnalyticsEventsSession.SESSION_RESTORED)
        assertEquals("session_token_expired", AnalyticsEventsSession.SESSION_TOKEN_EXPIRED)
        assertEquals("force_update_gate_shown", AnalyticsEventsSession.FORCE_UPDATE_GATE_SHOWN)
        assertEquals("force_update_tapped", AnalyticsEventsSession.FORCE_UPDATE_TAPPED)
        assertEquals("force_update_gate_blocking", AnalyticsEventsSession.FORCE_UPDATE_GATE_BLOCKING)
        assertEquals("route_entered", AnalyticsEventsSession.ROUTE_ENTERED)
        assertEquals("route_exited_via_back", AnalyticsEventsSession.ROUTE_EXITED_VIA_BACK)
        assertEquals("navigation_blocked", AnalyticsEventsSession.NAVIGATION_BLOCKED)
        assertEquals("shell_action", AnalyticsEventsSession.SHELL_ACTION)
        assertEquals("permission_gate_shown", AnalyticsEventsSession.PERMISSION_GATE_SHOWN)
        assertEquals("permission_gate_result", AnalyticsEventsSession.PERMISSION_GATE_RESULT)
    }

    @Test
    fun `param keys are the exact contract`() {
        assertEquals("permission", AnalyticsEventsSession.Params.PERMISSION)
        assertEquals("nav_route", AnalyticsEventsSession.Params.NAV_ROUTE)
        assertEquals("role", AnalyticsEventsSession.Params.ROLE)
        assertEquals("module_keys", AnalyticsEventsSession.Params.MODULE_KEYS)
        assertEquals("offline", AnalyticsEventsSession.Params.OFFLINE)
        assertEquals("shell_action", AnalyticsEventsSession.Params.SHELL_ACTION)
    }

    @Test
    fun `no event name collides with a Firebase-reserved name`() {
        val allNames = listOf(
            AnalyticsEventsSession.APP_BACKGROUNDED,
            AnalyticsEventsSession.SESSION_RESTORED,
            AnalyticsEventsSession.SESSION_TOKEN_EXPIRED,
            AnalyticsEventsSession.FORCE_UPDATE_GATE_SHOWN,
            AnalyticsEventsSession.FORCE_UPDATE_TAPPED,
            AnalyticsEventsSession.FORCE_UPDATE_GATE_BLOCKING,
            AnalyticsEventsSession.ROUTE_ENTERED,
            AnalyticsEventsSession.ROUTE_EXITED_VIA_BACK,
            AnalyticsEventsSession.NAVIGATION_BLOCKED,
            AnalyticsEventsSession.SHELL_ACTION,
            AnalyticsEventsSession.PERMISSION_GATE_SHOWN,
            AnalyticsEventsSession.PERMISSION_GATE_RESULT,
        )
        allNames.forEach { name ->
            assertFalse(
                "event name '$name' collides with a Firebase-reserved automatic event name and " +
                    "would be silently dropped — see AnalyticsEvents.SESSION_START's KDoc for the " +
                    "exact incident this guards against",
                name in firebaseReservedEventNames,
            )
        }
    }
}
