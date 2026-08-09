package sg.mesha.goatos.core.analytics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Guards the analytics taxonomy + context that egress and dashboards depend on. Event/property
 * names are a contract: renaming one silently breaks a funnel, so their exact string values are
 * pinned here on purpose (a rename must consciously update this test).
 */
class AnalyticsContractTest {

    @Test
    fun `event names are the exact snake_case contract`() {
        assertEquals("app_open", AnalyticsEvents.APP_OPEN)
        // NOT "session_start": that is a Firebase RESERVED name, and Firebase silently refused
        // every one of these events on device -- "Invalid public event name. Event will not be
        // logged (FE): session_start". The session-start event simply did not exist in analytics
        // until it was renamed. Keep the app_ prefix; reverting this reintroduces a permanently
        // dropped event that looks fine in code and produces nothing in the dashboard.
        assertEquals("app_session_start", AnalyticsEvents.SESSION_START)
        assertEquals("bootstrap_loaded", AnalyticsEvents.BOOTSTRAP_LOADED)
        assertEquals("login_attempt", AnalyticsEvents.LOGIN_ATTEMPT)
        assertEquals("login_success", AnalyticsEvents.LOGIN_SUCCESS)
        assertEquals("login_session_ready", AnalyticsEvents.LOGIN_SESSION_READY)
        assertEquals("login_failure", AnalyticsEvents.LOGIN_FAILURE)
        assertEquals("bootstrap_failed", AnalyticsEvents.BOOTSTRAP_FAILED)
        assertEquals("password_reset_requested", AnalyticsEvents.PASSWORD_RESET_REQUESTED)
        assertEquals("password_reset_sent", AnalyticsEvents.PASSWORD_RESET_SENT)
        assertEquals("sign_out", AnalyticsEvents.SIGN_OUT)
        assertEquals("feed_row_tapped", AnalyticsEvents.FEED_ROW_TAPPED)
        assertEquals("feed_transport_viewed", AnalyticsEvents.FEED_TRANSPORT_VIEWED)
        assertEquals("feed_transport_opened", AnalyticsEvents.FEED_TRANSPORT_OPENED)
        assertEquals("feed_transport_video_captured", AnalyticsEvents.FEED_TRANSPORT_VIDEO_CAPTURED)
        assertEquals("feed_transport_submitted", AnalyticsEvents.FEED_TRANSPORT_SUBMITTED)
        assertEquals("feed_transport_failure", AnalyticsEvents.FEED_TRANSPORT_FAILURE)
    }

    @Test
    fun `param and user-property keys are the exact contract`() {
        assertEquals("method", AnalyticsEvents.Params.METHOD)
        assertEquals("reason", AnalyticsEvents.Params.REASON)
        assertEquals("chrome", AnalyticsEvents.Params.CHROME)
        assertEquals("session_no", AnalyticsEvents.Params.SESSION_NO)
        assertEquals("email", AnalyticsEvents.Params.EMAIL)
        assertEquals("auth_uid", AnalyticsEvents.Params.FIREBASE_UID)
        assertEquals("role", AnalyticsEvents.UserProps.ROLE)
        assertEquals("email", AnalyticsEvents.UserProps.EMAIL)
        assertEquals("primary_park", AnalyticsEvents.UserProps.PRIMARY_PARK)
        assertEquals("park_id", AnalyticsEvents.UserProps.PARK_ID)
        assertEquals("flavor", AnalyticsEvents.UserProps.FLAVOR)
        assertEquals("tenant", AnalyticsEvents.UserProps.TENANT)
    }

    @Test
    fun `context starts identity-free and carries the build-fixed flavor`() {
        val ctx = AnalyticsContext(flavor = "dev")
        assertEquals("dev", ctx.flavor)
        assertNull("role is unknown until bootstrap resolves", ctx.role)
        assertNull("park is unknown until bootstrap resolves", ctx.parkScope)

        ctx.role = "operator"
        ctx.parkScope = "park-1"
        assertEquals("operator", ctx.role)
        assertEquals("park-1", ctx.parkScope)
    }

    @Test
    fun `noop analytics accepts events, properties, and user id without throwing`() {
        val analytics: AnalyticsPort = NoopAnalytics()
        // The whole point of the Noop is that no call path can throw or block.
        analytics.track(AnalyticsEvents.APP_OPEN)
        analytics.track(AnalyticsEvents.LOGIN_ATTEMPT, mapOf(AnalyticsEvents.Params.METHOD to "email"))
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, "operator")
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, null)
        analytics.setUserId("member-123")
        analytics.setUserId(null)
        assertTrue(true)
    }
}
