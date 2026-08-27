package sg.mesha.goatos.core.analytics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.OutboxTelemetryEvent
import sg.mesha.goatos.core.common.OutboxTerminalReason
import sg.mesha.goatos.core.common.OutboxWritePhase

/**
 * W-23: a queued write that dies must be LOUD, and a queue that is merely stalled must at least
 * be readable on the device. Every assertion here is on an EMITTED signal — never on the absence
 * of an error.
 */
class FailureReportingOutboxTelemetryReporterTest {

    private class RecordingCrashReporter : CrashReporter {
        val exceptions = mutableListOf<Pair<Throwable, String?>>()
        val breadcrumbs = mutableListOf<String>()
        override fun recordException(throwable: Throwable, message: String?) {
            exceptions += throwable to message
        }
        override fun log(message: String) { breadcrumbs += message }
        override fun setCustomKey(key: String, value: String) {}
    }

    private class RecordingAnalytics : AnalyticsPort {
        val events = mutableListOf<Pair<String, Map<String, String>>>()
        override fun track(event: String, props: Map<String, String>) { events += event to props }
        override fun setUserProperty(name: String, value: String?) {}
        override fun setUserId(id: String?) {}
    }

    private val crash = RecordingCrashReporter()
    private val analytics = RecordingAnalytics()
    private val logs = mutableListOf<String>()
    private var now = 0L

    private fun reporter() = FailureReportingOutboxTelemetryReporter(
        crashReporter = crash,
        analytics = analytics,
        nowMs = { now },
        logLine = { logs += it },
    )

    private fun event(
        phase: OutboxWritePhase,
        attempt: Int = 1,
        failureClass: String? = null,
        terminalReason: String? = null,
        retryInMs: Long? = null,
        groupKey: String = "",
        idempotencyKey: String = "",
        referencedProofOutboxItemId: String = "",
    ) = OutboxTelemetryEvent(
        phase = phase,
        opType = "WEIGHING_ANIMAL_OBSERVATION",
        itemId = "row-1",
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        referencedProofOutboxItemId = referencedProofOutboxItemId,
        attempt = attempt,
        maxAttempts = 5,
        failureClass = failureClass,
        terminalReason = terminalReason,
        retryInMs = retryInMs,
    )

    @Test
    fun `every phase reaches logcat and a crash breadcrumb`() {
        val reporter = reporter()
        OutboxWritePhase.entries.forEach { reporter.onOutboxWrite(event(it)) }

        assertEquals(OutboxWritePhase.entries.size, logs.size)
        assertEquals(OutboxWritePhase.entries.size, crash.breadcrumbs.size)
        assertTrue(logs.first().startsWith("outbox_write enqueued op=WEIGHING_ANIMAL_OBSERVATION"))
    }

    @Test
    fun `a stalled queue is readable on the device`() {
        val reporter = reporter()
        reporter.onOutboxWrite(event(OutboxWritePhase.ENQUEUED, attempt = 0))
        reporter.onOutboxWrite(event(OutboxWritePhase.ATTEMPT_STARTED))
        reporter.onOutboxWrite(event(OutboxWritePhase.ATTEMPT_FAILED, failureClass = "IOException"))
        reporter.onOutboxWrite(
            event(OutboxWritePhase.RETRY_SCHEDULED, failureClass = "IOException", retryInMs = 4_000),
        )

        assertEquals(
            listOf(
                "outbox_write enqueued op=WEIGHING_ANIMAL_OBSERVATION item=row-1 attempt=0/5",
                "outbox_write attempt_started op=WEIGHING_ANIMAL_OBSERVATION item=row-1 attempt=1/5",
                "outbox_write attempt_failed op=WEIGHING_ANIMAL_OBSERVATION item=row-1 attempt=1/5 failure=IOException",
                "outbox_write retry_scheduled op=WEIGHING_ANIMAL_OBSERVATION item=row-1 attempt=1/5 " +
                    "failure=IOException retry_in_ms=4000",
            ),
            logs,
        )
    }

    @Test
    fun `a failed attempt emits the attempt-failed analytics event`() {
        reporter().onOutboxWrite(event(OutboxWritePhase.ATTEMPT_FAILED, attempt = 3, failureClass = "IOException"))

        val (name, props) = analytics.events.single()
        assertEquals(AnalyticsEvents.SYNC_WRITE_ATTEMPT_FAILED, name)
        assertEquals("WEIGHING_ANIMAL_OBSERVATION", props[AnalyticsEvents.Params.OP_TYPE])
        assertEquals("3", props[AnalyticsEvents.Params.ATTEMPT])
        assertEquals("5", props[AnalyticsEvents.Params.MAX_ATTEMPTS])
        assertEquals("IOException", props[AnalyticsEvents.Params.REASON])
        assertTrue("a retryable attempt is not a non-fatal", crash.exceptions.isEmpty())
    }

    @Test
    fun `dependency wait emits diagnostic outbox and proof reference context`() {
        reporter().onOutboxWrite(
            event(
                OutboxWritePhase.DEPENDENCY_WAIT,
                failureClass = "ProofDependencyPendingException",
                retryInMs = 1_000,
                groupKey = "pc-care:task:task-1",
                idempotencyKey = "pc-care:task-proof:task-1:stock_fridge_video:proof-1",
                referencedProofOutboxItemId = "proof-1",
            ),
        )

        val (name, props) = analytics.events.single()
        assertEquals(AnalyticsEvents.SYNC_WRITE_DEPENDENCY_WAIT, name)
        assertEquals("row-1", props[AnalyticsEvents.Params.OUTBOX_ITEM_ID])
        assertEquals("pc-care:task:task-1", props[AnalyticsEvents.Params.GROUP_KEY])
        assertEquals("pc-care:task-proof:task-1:stock_fridge_video:proof-1", props[AnalyticsEvents.Params.IDEMPOTENCY_KEY])
        assertEquals("proof-1", props[AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])
        assertTrue(logs.single().contains("proof_outbox=proof-1"))
    }

    @Test
    fun `an exhausted transport failure is counted without opening a Crashlytics issue`() {
        reporter().onOutboxWrite(
            event(
                OutboxWritePhase.TERMINAL,
                attempt = 5,
                failureClass = "HttpException",
                terminalReason = OutboxTerminalReason.ATTEMPTS_EXHAUSTED,
            ),
        )

        val (name, props) = analytics.events.single()
        assertEquals(AnalyticsEvents.SYNC_WRITE_DEAD, name)
        assertEquals(OutboxTerminalReason.ATTEMPTS_EXHAUSTED, props[AnalyticsEvents.Params.REASON])
        assertTrue(crash.exceptions.isEmpty())
    }

    @Test
    fun `an exhausted client defect is loud - non-fatal plus analytics`() {
        reporter().onOutboxWrite(
            event(
                OutboxWritePhase.TERMINAL,
                attempt = 5,
                failureClass = "IllegalStateException",
                terminalReason = OutboxTerminalReason.ATTEMPTS_EXHAUSTED,
            ),
        )

        val (throwable, message) = crash.exceptions.single()
        assertTrue(throwable is FailureReportingOutboxTelemetryReporter.DeadQueuedWrite)
        assertTrue(message!!.contains("terminal_reason=attempts_exhausted"))
    }

    @Test
    fun `a conflict-dead transport failure is counted without opening a Crashlytics issue`() {
        reporter().onOutboxWrite(
            event(
                OutboxWritePhase.TERMINAL,
                attempt = 1,
                failureClass = "HttpException",
                terminalReason = OutboxTerminalReason.CONFLICT,
            ),
        )

        assertEquals(
            OutboxTerminalReason.CONFLICT,
            analytics.events.single().second[AnalyticsEvents.Params.REASON],
        )
        assertTrue(crash.exceptions.isEmpty())
    }

    @Test
    fun `a conflict-dead client defect is loud`() {
        reporter().onOutboxWrite(
            event(
                OutboxWritePhase.TERMINAL,
                attempt = 1,
                failureClass = "NonRetryableSyncException",
                terminalReason = OutboxTerminalReason.CONFLICT,
            ),
        )

        assertEquals(
            OutboxTerminalReason.CONFLICT,
            analytics.events.single().second[AnalyticsEvents.Params.REASON],
        )
        val (throwable, message) = crash.exceptions.single()
        assertTrue(throwable is FailureReportingOutboxTelemetryReporter.DeadQueuedWrite)
        assertTrue(message!!.contains("failure=NonRetryableSyncException"))
    }

    @Test
    fun `a storm of identical exhausted deaths is throttled to one non-fatal per minute`() {
        val reporter = reporter()
        val dead = event(
            OutboxWritePhase.TERMINAL,
            attempt = 5,
            failureClass = "IllegalStateException",
            terminalReason = OutboxTerminalReason.ATTEMPTS_EXHAUSTED,
        )
        repeat(20) { reporter.onOutboxWrite(dead) }

        assertEquals("the count is the signal — every death still emits an event", 20, analytics.events.size)
        assertEquals("every death remains visible in logcat", 20, logs.size)
        assertEquals("every death remains a Crashlytics breadcrumb", 20, crash.breadcrumbs.size)
        assertEquals("but only one non-fatal", 1, crash.exceptions.size)

        now += FailureReportingOutboxTelemetryReporter.NON_FATAL_THROTTLE_MS
        reporter.onOutboxWrite(dead)
        assertEquals("a still-dying queue re-reports after the window", 2, crash.exceptions.size)
    }

    @Test
    fun `a different op type is still counted separately in telemetry`() {
        val reporter = reporter()
        val dead = event(
            OutboxWritePhase.TERMINAL,
            attempt = 5,
            failureClass = "IllegalStateException",
            terminalReason = OutboxTerminalReason.ATTEMPTS_EXHAUSTED,
        )
        reporter.onOutboxWrite(dead)
        reporter.onOutboxWrite(dead.copy(opType = "PROOF_UPLOAD"))

        assertEquals(2, analytics.events.size)
        assertTrue(logs.any { it.contains("op=PROOF_UPLOAD") })
        assertEquals(2, crash.exceptions.size)
    }

    @Test
    fun `a broken analytics seam never propagates out of the reporter`() {
        val exploding = object : AnalyticsPort {
            override fun track(event: String, props: Map<String, String>) = error("boom")
            override fun setUserProperty(name: String, value: String?) {}
            override fun setUserId(id: String?) {}
        }
        FailureReportingOutboxTelemetryReporter(crash, exploding, { now }, { logs += it })
            .onOutboxWrite(
                event(
                    OutboxWritePhase.TERMINAL,
                    failureClass = "HttpException",
                    terminalReason = OutboxTerminalReason.CONFLICT,
                ),
            )

        assertEquals("the logcat line still landed before the throw", 1, logs.size)
    }
}
