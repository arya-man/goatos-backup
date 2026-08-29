package sg.mesha.goatos.core.analytics

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * [DeadControlWatchdog] is the seam that turns "the operator tapped a control and nothing
 * observably happened" into a distinct, reportable signal (docs/observability/
 * TELEMETRY_GUARDRAILS.md). These tests prove the two rules the class-level doc promises hold
 * under virtual time, with no real sleeps:
 *  - intent and dead-control are never reported one without the other,
 *  - a second [DeadControlWatchdog.armIntent] replaces (never doubles) the pending timer,
 *  - [DeadControlWatchdog.cancel] (mirroring ViewModel `onCleared()`) prevents any later fire.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class TelemetryWatchdogTest {

    private val timeoutMs = 1500L

    @Test
    fun `disarming within the window emits the intent event and no dead-control event`() = runTest {
        val analytics = RecordingWatchdogAnalytics()
        val crashReporter = RecordingWatchdogCrashReporter()
        val watchdog = DeadControlWatchdog(
            analytics = analytics,
            crashReporter = crashReporter,
            scope = TestScope(testScheduler),
            intentEvent = INTENT_EVENT,
            deadControlEvent = DEAD_CONTROL_EVENT,
        )

        watchdog.armIntent(mapOf("proof_id" to "proof-1"), timeoutMs)
        assertEquals(listOf(INTENT_EVENT), analytics.events.map { it.first })

        // Outcome callback lands well inside the window.
        advanceTimeBy(timeoutMs / 2)
        watchdog.disarm()

        // Let any (incorrectly) still-pending timer run out.
        advanceUntilIdle()

        assertEquals(listOf(INTENT_EVENT), analytics.events.map { it.first })
        assertTrue("no dead-control event should have fired", crashReporter.recorded.isEmpty())
    }

    @Test
    fun `arming and never disarming emits the dead-control event and breadcrumb`() = runTest {
        val analytics = RecordingWatchdogAnalytics()
        val crashReporter = RecordingWatchdogCrashReporter()
        val watchdog = DeadControlWatchdog(
            analytics = analytics,
            crashReporter = crashReporter,
            scope = TestScope(testScheduler),
            intentEvent = INTENT_EVENT,
            deadControlEvent = DEAD_CONTROL_EVENT,
        )

        watchdog.armIntent(mapOf("proof_id" to "proof-2"), timeoutMs)
        advanceTimeBy(timeoutMs)
        advanceUntilIdle()

        assertEquals(listOf(INTENT_EVENT, DEAD_CONTROL_EVENT), analytics.events.map { it.first })
        assertEquals(listOf("dead-control watchdog fired: $DEAD_CONTROL_EVENT"), crashReporter.logs)
        assertTrue(crashReporter.recorded.isEmpty())
    }

    @Test
    fun `a second armIntent replaces the pending timer instead of double-reporting`() = runTest {
        val analytics = RecordingWatchdogAnalytics()
        val crashReporter = RecordingWatchdogCrashReporter()
        val watchdog = DeadControlWatchdog(
            analytics = analytics,
            crashReporter = crashReporter,
            scope = TestScope(testScheduler),
            intentEvent = INTENT_EVENT,
            deadControlEvent = DEAD_CONTROL_EVENT,
        )

        // A mashed button: two taps in quick succession, both well inside the timeout window.
        watchdog.armIntent(mapOf("proof_id" to "proof-3"), timeoutMs)
        advanceTimeBy(timeoutMs / 2)
        watchdog.armIntent(mapOf("proof_id" to "proof-3"), timeoutMs)

        // The first watchdog's original deadline (now passed) must NOT have fired.
        assertEquals(listOf(INTENT_EVENT, INTENT_EVENT), analytics.events.map { it.first })
        assertTrue(crashReporter.recorded.isEmpty())

        // Let the SECOND watchdog's own full window elapse.
        advanceTimeBy(timeoutMs)
        advanceUntilIdle()

        // Repeated intents, but exactly one dead-control signal — not two.
        assertEquals(
            listOf(INTENT_EVENT, INTENT_EVENT, DEAD_CONTROL_EVENT),
            analytics.events.map { it.first },
        )
        assertEquals(listOf("dead-control watchdog fired: $DEAD_CONTROL_EVENT"), crashReporter.logs)
        assertTrue(crashReporter.recorded.isEmpty())
    }

    @Test
    fun `cancel prevents any later fire`() = runTest {
        val analytics = RecordingWatchdogAnalytics()
        val crashReporter = RecordingWatchdogCrashReporter()
        val watchdog = DeadControlWatchdog(
            analytics = analytics,
            crashReporter = crashReporter,
            scope = TestScope(testScheduler),
            intentEvent = INTENT_EVENT,
            deadControlEvent = DEAD_CONTROL_EVENT,
        )

        watchdog.armIntent(mapOf("proof_id" to "proof-4"), timeoutMs)
        // Simulates ViewModel onCleared() / screen-exit before the outcome ever lands.
        watchdog.cancel()

        advanceUntilIdle()

        assertEquals(listOf(INTENT_EVENT), analytics.events.map { it.first })
        assertTrue(
            "a torn-down screen must never record a dead-control report",
            crashReporter.recorded.isEmpty(),
        )
    }
}

private const val INTENT_EVENT = "verify_video_play_intent"
private const val DEAD_CONTROL_EVENT = "verify_video_play_dead_control"

private class RecordingWatchdogAnalytics : AnalyticsPort {
    val events = mutableListOf<Pair<String, Map<String, String>>>()

    override fun track(event: String, props: Map<String, String>) {
        events.add(event to props)
    }

    override fun setUserProperty(name: String, value: String?) {}

    override fun setUserId(id: String?) {}
}

private class RecordingWatchdogCrashReporter : CrashReporter {
    val recorded = mutableListOf<Pair<Throwable, String?>>()
    val logs = mutableListOf<String>()

    override fun recordException(throwable: Throwable, message: String?) {
        recorded.add(throwable to message)
    }

    override fun log(message: String) {
        logs += message
    }

    override fun setCustomKey(key: String, value: String) {}
}
