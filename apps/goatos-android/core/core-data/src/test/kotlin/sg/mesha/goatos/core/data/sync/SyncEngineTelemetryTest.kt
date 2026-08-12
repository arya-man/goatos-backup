package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.OutboxTelemetryEvent
import sg.mesha.goatos.core.common.OutboxTelemetryReporter
import sg.mesha.goatos.core.common.OutboxTerminalReason
import sg.mesha.goatos.core.common.OutboxWritePhase
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.ValidationIssueDto
import sg.mesha.goatos.core.network.dto.ValidationReportDto
import java.io.IOException
import retrofit2.HttpException

/**
 * W-23: driving an outbox row to each of its states must EMIT the corresponding lifecycle signal.
 *
 * Before this, the drain loop was completely silent: a write could retry for minutes and produce
 * nothing on-device — no logcat line, no crash breadcrumb, no analytics event — so a stuck upload
 * could only be diagnosed from the server log and the database. These assertions are on emitted
 * events, never on the absence of an error.
 */
class SyncEngineTelemetryTest {

    private class RecordingTelemetry : OutboxTelemetryReporter {
        val events = mutableListOf<OutboxTelemetryEvent>()
        override fun onOutboxWrite(event: OutboxTelemetryEvent) { events += event }
        fun phases() = events.map { it.phase }
        fun first(phase: OutboxWritePhase) = events.first { it.phase == phase }
    }

    private fun queuedShedSubmit(
        id: String = "row-1",
        maxAttempts: Int = 3,
        attemptCount: Int = 0,
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.SHED_SUBMIT.name,
        groupKey = "shed-1",
        idempotencyKey = "key-$id",
        payloadJson = syncJson.encodeToString(
            ShedSubmitPayload(
                taskId = "task-1",
                request = SubmitTaskRequestDto(sopVersionId = "sop-1", idempotencyKey = "key-$id"),
            ),
        ),
        requestFingerprint = "",
        status = OutboxStatus.QUEUED.name,
        attemptCount = attemptCount,
        maxAttempts = maxAttempts,
        conflict = false,
        createdAt = 0L,
        updatedAt = 0L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = null,
    )

    private fun engine(store: FakeOutboxStore, api: ScriptedAppApi, telemetry: RecordingTelemetry) =
        SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, telemetry = telemetry)

    @Test
    fun `a successful drain announces the attempt it started`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val telemetry = RecordingTelemetry()

        engine(store, ScriptedAppApi(), telemetry).drainOnce()

        assertEquals(listOf(OutboxWritePhase.ATTEMPT_STARTED), telemetry.phases())
        val started = telemetry.first(OutboxWritePhase.ATTEMPT_STARTED)
        assertEquals(OutboxOpType.SHED_SUBMIT.name, started.opType)
        assertEquals(1, started.attempt)
        assertEquals(3, started.maxAttempts)
    }

    @Test
    fun `a retryable transport failure announces the failure and the booked retry`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("network down") }
        }
        val telemetry = RecordingTelemetry()

        engine(store, api, telemetry).drainOnce()

        assertEquals(
            listOf(
                OutboxWritePhase.ATTEMPT_STARTED,
                OutboxWritePhase.ATTEMPT_FAILED,
                OutboxWritePhase.RETRY_SCHEDULED,
            ),
            telemetry.phases(),
        )
        val failed = telemetry.first(OutboxWritePhase.ATTEMPT_FAILED)
        assertEquals("IOException", failed.failureClass)
        assertEquals(1, failed.attempt)
        val retry = telemetry.first(OutboxWritePhase.RETRY_SCHEDULED)
        assertTrue("the operator's phone can now say how far out the retry is", retry.retryInMs!! > 0)
    }

    @Test
    fun `exhausting the attempts announces TERMINAL - the write that will never be sent`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(maxAttempts = 1))
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("network down") }
        }
        val telemetry = RecordingTelemetry()

        engine(store, api, telemetry).drainOnce()

        assertTrue(OutboxWritePhase.TERMINAL in telemetry.phases())
        assertTrue(OutboxWritePhase.RETRY_SCHEDULED !in telemetry.phases())
        val terminal = telemetry.first(OutboxWritePhase.TERMINAL)
        assertEquals(OutboxTerminalReason.ATTEMPTS_EXHAUSTED, terminal.terminalReason)
        assertEquals("IOException", terminal.failureClass)
        assertEquals(1, terminal.attempt)
    }

    @Test
    fun `a definitive server rejection announces TERMINAL as a conflict`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ ->
                SubmissionResponseDto(
                    submission = SubmissionSummaryDto(
                        validationReport = ValidationReportDto(
                            valid = false,
                            errors = listOf(ValidationIssueDto(code = "bad", message = "Not allowed")),
                        ),
                    ),
                )
            }
        }
        val telemetry = RecordingTelemetry()

        engine(store, api, telemetry).drainOnce()

        val terminal = telemetry.first(OutboxWritePhase.TERMINAL)
        assertEquals(OutboxTerminalReason.CONFLICT, terminal.terminalReason)
        assertEquals("NonRetryableSyncException", terminal.failureClass)
        assertTrue(
            "the server's own copy stays in the row for the operator — it must not ride the telemetry seam",
            telemetry.events.none { it.toString().contains("Not allowed") },
        )
    }

    @Test
    fun `auth and access errors terminalize immediately instead of exhausting retry budget`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(maxAttempts = 8))
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw HttpException(403) }
        }
        val telemetry = RecordingTelemetry()

        engine(store, api, telemetry).drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertTrue(row.conflict)
        assertEquals(1, row.attemptCount)
        assertEquals(Long.MAX_VALUE, row.nextAttemptAt)
        assertEquals(
            listOf(
                OutboxWritePhase.ATTEMPT_STARTED,
                OutboxWritePhase.ATTEMPT_FAILED,
                OutboxWritePhase.TERMINAL,
            ),
            telemetry.phases(),
        )
        assertTrue(OutboxWritePhase.RETRY_SCHEDULED !in telemetry.phases())
        val terminal = telemetry.first(OutboxWritePhase.TERMINAL)
        assertEquals(OutboxTerminalReason.CONFLICT, terminal.terminalReason)
        assertEquals("HttpException", terminal.failureClass)
        assertEquals(1, terminal.attempt)
    }

    @Test
    fun `a broken reporter never fails the write it is reporting on`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val engine = SyncEngine(
            store,
            ScriptedAppApi(),
            connectivityGate = { true },
            clock = { 0L },
            telemetry = { error("telemetry exploded") },
        )

        engine.drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-1")!!.status)
    }

    @Test
    fun `a newly queued write announces ENQUEUED before anything is attempted`() = runBlocking {
        val store = FakeOutboxStore()
        val telemetry = RecordingTelemetry()
        val repository = DefaultSyncRepository(
            store = store,
            engine = SyncEngine(store, ScriptedAppApi(), connectivityGate = { false }, clock = { 0L }),
            connectivityGate = { false },
            appScope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.Unconfined),
            clock = { 0L },
            telemetry = telemetry,
        )

        repository.enqueueShedSubmit(
            taskId = "task-1",
            groupKey = "shed-1",
            idempotencyKey = "key-1",
            request = SubmitTaskRequestDto(sopVersionId = "sop-1", idempotencyKey = "key-1"),
        )

        val enqueued = telemetry.first(OutboxWritePhase.ENQUEUED)
        assertEquals(OutboxOpType.SHED_SUBMIT.name, enqueued.opType)
        assertEquals(0, enqueued.attempt)
    }

    @Test
    fun `an idempotent re-enqueue of the same write is not announced twice`() = runBlocking {
        val store = FakeOutboxStore()
        val telemetry = RecordingTelemetry()
        val repository = DefaultSyncRepository(
            store = store,
            engine = SyncEngine(store, ScriptedAppApi(), connectivityGate = { false }, clock = { 0L }),
            connectivityGate = { false },
            appScope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.Unconfined),
            clock = { 0L },
            telemetry = telemetry,
        )
        val request = SubmitTaskRequestDto(sopVersionId = "sop-1", idempotencyKey = "key-1")

        repeat(3) {
            repository.enqueueShedSubmit("task-1", "shed-1", "key-1", request)
        }

        assertEquals(1, telemetry.events.count { it.phase == OutboxWritePhase.ENQUEUED })
    }
}
