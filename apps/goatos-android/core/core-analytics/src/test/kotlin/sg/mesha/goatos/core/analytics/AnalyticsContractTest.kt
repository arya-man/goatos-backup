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
        assertEquals("session_start", AnalyticsEvents.SESSION_START)
        assertEquals("bootstrap_loaded", AnalyticsEvents.BOOTSTRAP_LOADED)
        assertEquals("login_attempt", AnalyticsEvents.LOGIN_ATTEMPT)
        assertEquals("login_success", AnalyticsEvents.LOGIN_SUCCESS)
        assertEquals("login_failure", AnalyticsEvents.LOGIN_FAILURE)
        assertEquals("password_reset_requested", AnalyticsEvents.PASSWORD_RESET_REQUESTED)
        assertEquals("password_reset_sent", AnalyticsEvents.PASSWORD_RESET_SENT)
        assertEquals("sign_out", AnalyticsEvents.SIGN_OUT)
    }

    @Test
    fun `param and user-property keys are the exact contract`() {
        assertEquals("method", AnalyticsEvents.Params.METHOD)
        assertEquals("reason", AnalyticsEvents.Params.REASON)
        assertEquals("chrome", AnalyticsEvents.Params.CHROME)
        assertEquals("role", AnalyticsEvents.UserProps.ROLE)
        assertEquals("primary_park", AnalyticsEvents.UserProps.PRIMARY_PARK)
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
    fun `noop analytics accepts events and properties without throwing`() {
        val analytics: AnalyticsPort = NoopAnalytics()
        // The whole point of the Noop is that no call path can throw or block.
        analytics.track(AnalyticsEvents.APP_OPEN)
        analytics.track(AnalyticsEvents.LOGIN_ATTEMPT, mapOf(AnalyticsEvents.Params.METHOD to "email"))
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, "operator")
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, null)
        assertTrue(true)
    }
}
