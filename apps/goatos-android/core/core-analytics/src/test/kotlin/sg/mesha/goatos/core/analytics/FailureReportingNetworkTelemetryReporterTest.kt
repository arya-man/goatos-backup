package sg.mesha.goatos.core.analytics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.NetworkTelemetryEvent
import sg.mesha.goatos.core.network.NetworkTelemetryReporter

/**
 * Regression tests for the night three real failures produced NO client-side signal: a 409 the
 * CEO saw as a bare "HTTP 409 Conflict", a park-scoped 403, and ~20 HTTP 500s in a retry storm
 * that left logcat completely silent.
 */
class FailureReportingNetworkTelemetryReporterTest {

    private class RecordingCrashReporter : CrashReporter {
        val breadcrumbs = mutableListOf<String>()
        val nonFatals = mutableListOf<Throwable>()
        override fun recordException(throwable: Throwable, message: String?) { nonFatals += throwable }
        override fun log(message: String) { breadcrumbs += message }
        override fun setCustomKey(key: String, value: String) = Unit
    }

    private class RecordingAnalytics : AnalyticsPort {
        val events = mutableListOf<Pair<String, Map<String, String>>>()
        override fun track(event: String, props: Map<String, String>) { events += event to props }
        override fun setUserProperty(name: String, value: String?) = Unit
        override fun setUserId(id: String?) = Unit
    }

    private val delegateCalls = mutableListOf<NetworkTelemetryEvent>()
    private val delegate = NetworkTelemetryReporter { delegateCalls += it }
    private val crash = RecordingCrashReporter()
    private val analytics = RecordingAnalytics()
    private val logs = mutableListOf<String>()
    private var clock = 0L

    private fun reporter() = FailureReportingNetworkTelemetryReporter(
        delegate = delegate,
        crashReporter = crash,
        analytics = analytics,
        nowMs = { clock },
        logLine = { logs += it },
    )

    private fun event(status: Int, route: String = "/weighing/campaigns", method: String = "POST") =
        NetworkTelemetryEvent(
            method = method,
            route = route,
            statusCode = status,
            durationMs = 84,
            traceparent = "00-aabb-ccdd-01",
        )

    @Test
    fun `409 emits logcat, breadcrumb, non-fatal and an api_call_failure event`() {
        reporter().onNetworkCall(event(409))

        assertEquals(1, logs.size)
        assertTrue(logs.first(), logs.first().contains("POST /weighing/campaigns"))
        assertTrue(logs.first(), logs.first().contains("status=409"))
        assertEquals(1, crash.breadcrumbs.size)
        assertEquals(1, crash.nonFatals.size)

        val (name, props) = analytics.events.single()
        assertEquals(AnalyticsEvents.API_CALL_FAILURE, name)
        assertEquals("POST", props[AnalyticsEvents.Params.METHOD])
        assertEquals("/weighing/campaigns", props[AnalyticsEvents.Params.ROUTE])
        assertEquals("409", props[AnalyticsEvents.Params.STATUS_CODE])
        assertEquals("84", props[AnalyticsEvents.Params.DURATION_MS])
    }

    @Test
    fun `403 on a park-scoped read is reported the same way`() {
        reporter().onNetworkCall(event(403, route = "/app/parks/{id}/sheds", method = "GET"))

        assertEquals("403", analytics.events.single().second[AnalyticsEvents.Params.STATUS_CODE])
        assertEquals(1, crash.nonFatals.size)
        assertTrue(logs.first(), logs.first().contains("GET /app/parks/{id}/sheds"))
    }

    @Test
    fun `a 20-call 500 retry storm logs 20 lines and 20 events but only one non-fatal`() {
        val subject = reporter()
        repeat(20) { subject.onNetworkCall(event(500, route = "/app/proof/{id}", method = "GET")) }

        assertEquals("every retry must be visible in logcat", 20, logs.size)
        assertEquals("every retry must be visible as an event", 20, analytics.events.size)
        assertEquals("every retry must be a crash breadcrumb", 20, crash.breadcrumbs.size)
        assertEquals("non-fatals are throttled so the console is not buried", 1, crash.nonFatals.size)
    }

    @Test
    fun `the same failure is reported again after the throttle window`() {
        val subject = reporter()
        subject.onNetworkCall(event(500))
        clock += FailureReportingNetworkTelemetryReporter.NON_FATAL_THROTTLE_MS
        subject.onNetworkCall(event(500))

        assertEquals(2, crash.nonFatals.size)
    }

    @Test
    fun `a call that threw before any response is a failure too`() {
        reporter().onNetworkCall(event(-1).copy(failureClass = "UnknownHostException"))

        assertTrue(logs.first(), logs.first().contains("status=no_response"))
        assertTrue(logs.first(), logs.first().contains("failure=UnknownHostException"))
        assertEquals("-1", analytics.events.single().second[AnalyticsEvents.Params.STATUS_CODE])
        assertTrue("offline DNS failures stay out of Crashlytics issues", crash.nonFatals.isEmpty())
    }

    @Test
    fun `device register 401 keeps breadcrumbs and analytics but not a non-fatal`() {
        reporter().onNetworkCall(event(401, route = "/app/devices/register", method = "POST"))

        assertEquals(1, logs.size)
        assertEquals(1, crash.breadcrumbs.size)
        assertEquals("401", analytics.events.single().second[AnalyticsEvents.Params.STATUS_CODE])
        assertTrue(crash.nonFatals.isEmpty())
    }

    @Test
    fun `analytics endpoint failure keeps local signal only`() {
        reporter().onNetworkCall(event(403, route = "/app/analytics/events", method = "POST"))

        assertEquals(1, logs.size)
        assertEquals(1, crash.breadcrumbs.size)
        assertTrue(crash.nonFatals.isEmpty())
        assertTrue(analytics.events.isEmpty())
    }

    @Test
    fun `auth session telemetry endpoint failure keeps local signal only`() {
        reporter().onNetworkCall(event(403, route = "/auth/session-events", method = "POST"))

        assertEquals(1, logs.size)
        assertEquals(1, crash.breadcrumbs.size)
        assertTrue(crash.nonFatals.isEmpty())
        assertTrue(analytics.events.isEmpty())
    }

    @Test
    fun `ordinary 401 remains a non-fatal`() {
        reporter().onNetworkCall(event(401, route = "/app/bootstrap", method = "GET"))

        assertEquals(1, crash.nonFatals.size)
    }

    @Test
    fun `successful calls reach the delegate and produce no failure signal`() {
        reporter().onNetworkCall(event(200))

        assertEquals(1, delegateCalls.size)
        assertTrue(logs.isEmpty())
        assertTrue(crash.breadcrumbs.isEmpty())
        assertTrue(crash.nonFatals.isEmpty())
        assertTrue(analytics.events.isEmpty())
    }

    @Test
    fun `the delegate still runs for a failed call`() {
        reporter().onNetworkCall(event(409))

        assertEquals(1, delegateCalls.size)
    }

    @Test
    fun `a throwing delegate never suppresses the failure report`() {
        FailureReportingNetworkTelemetryReporter(
            delegate = { error("firebase perf unavailable") },
            crashReporter = crash,
            analytics = analytics,
            nowMs = { clock },
            logLine = { logs += it },
        ).onNetworkCall(event(409))

        assertEquals(1, logs.size)
        assertEquals(1, analytics.events.size)
    }

    @Test
    fun `no credential or body content is ever in the reported summary`() {
        reporter().onNetworkCall(event(401))

        val emitted = logs + crash.breadcrumbs + analytics.events.flatMap { it.second.values }
        for (line in emitted) {
            assertTrue(line, !line.contains("Bearer", ignoreCase = true))
            assertTrue(line, !line.contains("token", ignoreCase = true))
        }
    }
}
