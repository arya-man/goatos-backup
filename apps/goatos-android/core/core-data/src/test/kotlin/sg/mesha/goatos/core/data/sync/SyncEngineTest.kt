package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.ValidationIssueDto
import sg.mesha.goatos.core.network.dto.ValidationReportDto
import java.io.IOException

/** Table-driven coverage of the outbox drain contract: enqueue -> drain, idempotent retry
 *  (same key reused), failure/backoff, dead-letter, and the non-retryable business-conflict
 *  path. Uses [FakeOutboxStore] + [ScriptedAppApi] only — no Room/Robolectric (see
 *  [FakeOutboxStore]'s KDoc for why). */
class SyncEngineTest {

    private fun queuedShedSubmit(
        id: String = "row-1",
        groupKey: String = "shed-1",
        idempotencyKey: String = "key-1",
        createdAt: Long = 0L,
        maxAttempts: Int = DEFAULT_MAX_ATTEMPTS,
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.SHED_SUBMIT.name,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ShedSubmitPayload(
                taskId = "task-1",
                request = SubmitTaskRequestDto(sopVersionId = "sop-1", idempotencyKey = idempotencyKey),
            ),
        ),
        status = OutboxStatus.QUEUED.name,
        attemptCount = 0,
        maxAttempts = maxAttempts,
        conflict = false,
        createdAt = createdAt,
        updatedAt = createdAt,
        nextAttemptAt = createdAt,
        lastError = null,
        resultJson = null,
    )

    private fun okSubmission() =
        SubmissionResponseDto(submission = SubmissionSummaryDto(validationReport = ValidationReportDto(valid = true)))

    @Test
    fun `happy path drains a queued item to SUCCEEDED`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals(1, api.submitCalls.size)
    }

    @Test
    fun `retry after a transport failure reuses the SAME idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(idempotencyKey = "stable-key"))
        var calls = 0
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ ->
                calls++
                if (calls == 1) throw IOException("network down")
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, backoff = BackoffPolicy { 0L })

        engine.drainOnce() // attempt 1: transport failure, scheduled for retry
        var row = store.findById("row-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertEquals(1, row.attemptCount)
        assertTrue(!row.conflict)

        engine.drainOnce() // attempt 2: succeeds
        row = store.findById("row-1")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)

        assertEquals(listOf("stable-key", "stable-key"), api.submitCalls.map { it.second })
    }

    @Test
    fun `transport failure schedules the next retry at the computed backoff time`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(idempotencyKey = "stable-key"))
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("network down") }
        }
        var scheduledAt: Long? = null
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { true },
            clock = { 1_000L },
            backoff = BackoffPolicy { 60_000L },
            retryScheduler = SyncRetryScheduler { scheduledAt = it },
        )

        engine.drainOnce()

        assertEquals(61_000L, scheduledAt)
    }

    @Test
    fun `exhausting the attempt budget marks the row dead-letter and excludes it from future drains`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(maxAttempts = 3))
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("still down") }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, backoff = BackoffPolicy { 0L })

        repeat(3) { engine.drainOnce() }

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertEquals(3, row.attemptCount)
        assertTrue(!row.conflict)
        assertEquals(Long.MAX_VALUE, row.nextAttemptAt)

        engine.drainOnce() // 4th pass must NOT attempt the now-dead-lettered row again.
        assertEquals(3, api.submitCalls.size)
    }

    @Test
    fun `a definitive validation rejection is terminal on the FIRST attempt, not a retryable failure`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ ->
                SubmissionResponseDto(
                    submission = SubmissionSummaryDto(
                        validationReport = ValidationReportDto(
                            valid = false,
                            errors = listOf(ValidationIssueDto(field = "answers", code = "required", message = "Answers required")),
                        ),
                    ),
                )
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertTrue(row.conflict)
        assertEquals(1, row.attemptCount) // no retry budget spent looping — terminal immediately
        // "required" is a form-gap code -> the engine substitutes the honest, friendly
        // "form capture lands later" message rather than surfacing the raw field error.
        assertEquals(
            "This drive needs the recording form before it can be submitted — form capture lands in a later build.",
            row.lastError,
        )

        engine.drainOnce() // a conflict row is never auto-eligible again.
        assertEquals(1, api.submitCalls.size)
    }

    @Test
    fun `a row stranded IN_FLIGHT by a crash is reclaimed and drained on the next pass`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        // Simulate a process death mid-dispatch: the row was marked IN_FLIGHT, then the process
        // died before markSucceeded/markFailed ran. Pre-fix, eligibleForDrain excluded IN_FLIGHT
        // rows forever, so this submission would be stranded (never retried).
        store.markInFlight("row-1", now = 5L)
        assertEquals(OutboxStatus.IN_FLIGHT.name, store.findById("row-1")!!.status)
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 10L })

        engine.drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals(1, api.submitCalls.size)
    }

    @Test
    fun `offline skips the drain entirely without spending an attempt`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { false }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.QUEUED.name, row.status)
        assertEquals(0, row.attemptCount)
        assertEquals(0, api.submitCalls.size)
    }

    @Test
    fun `items in the same group drain strictly oldest-first`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(id = "row-2", groupKey = "shed-1", idempotencyKey = "key-2", createdAt = 200L))
        store.insert(queuedShedSubmit(id = "row-1", groupKey = "shed-1", idempotencyKey = "key-1", createdAt = 100L))
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, request ->
                order += request.idempotencyKey
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L })

        engine.drainOnce()

        assertEquals(listOf("key-1", "key-2"), order)
    }

    @Test
    fun `same group stops after the oldest item fails`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(id = "row-2", groupKey = "shed-1", idempotencyKey = "key-2", createdAt = 200L))
        store.insert(queuedShedSubmit(id = "row-1", groupKey = "shed-1", idempotencyKey = "key-1", createdAt = 100L))
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, key, _ ->
                order += key
                if (key == "key-1") throw IOException("oldest failed")
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L }, backoff = BackoffPolicy { 60_000L })

        engine.drainOnce()

        assertEquals(listOf("key-1"), order)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-2")!!.status)
    }

    @Test
    fun `newer same-group rows stay blocked on a later drain while oldest is backed off`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(id = "row-2", groupKey = "shed-1", idempotencyKey = "key-2", createdAt = 200L))
        store.insert(queuedShedSubmit(id = "row-1", groupKey = "shed-1", idempotencyKey = "key-1", createdAt = 100L))
        var now = 1_000L
        var failOldest = true
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, key, _ ->
                order += key
                if (key == "key-1" && failOldest) {
                    failOldest = false
                    throw IOException("oldest failed")
                }
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { now }, backoff = BackoffPolicy { 60_000L })

        engine.drainOnce()
        engine.drainOnce()

        assertEquals(listOf("key-1"), order)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-2")!!.status)

        now = 61_000L
        engine.drainOnce()

        assertEquals(listOf("key-1", "key-1", "key-2"), order)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-2")!!.status)
    }

    @Test
    fun `dispatches a RESCHEDULE item via the reschedule endpoint with its idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        val idempotencyKey = "resched-key-1"
        store.insert(
            OutboxEntity(
                id = "row-r1",
                opType = OutboxOpType.RESCHEDULE.name,
                groupKey = "obl-1",
                idempotencyKey = idempotencyKey,
                payloadJson = syncJson.encodeToString(
                    ReschedulePayload(obligationId = "obl-1", request = RescheduleObligationRequestDto(dueAt = "2026-07-10")),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        var seenKey: String? = null
        val api = ScriptedAppApi().apply {
            rescheduleObligationFn = { obligationId, key, _ ->
                seenKey = key
                RescheduleObligationResponseDto(obligationId = obligationId, idempotentReplay = false)
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        assertEquals(idempotencyKey, seenKey)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-r1")!!.status)
    }

    @Test
    fun `dispatches a PROOF_UPLOAD item via the registerProof endpoint`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(
            OutboxEntity(
                id = "row-p1",
                opType = OutboxOpType.PROOF_UPLOAD.name,
                groupKey = "shed-1",
                idempotencyKey = "proof-key-1",
                payloadJson = syncJson.encodeToString(
                    ProofUploadPayload(request = ProofUploadRequestDto(scopeType = "shed", scopeId = "shed-1", subjectType = "shed")),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-p1")!!.status)
    }

    // --- Batching + Boolean drain-pass contract -------------------------------------------------

    @Test
    fun `more eligible rows than the batch size drain across several bounded fetches`() = runBlocking {
        val store = RecordingOutboxStore()
        // 250 > the engine's batch size, spread over a few groups so the drain also fans out.
        repeat(250) { i ->
            store.insert(queuedShedSubmit(id = "row-$i", idempotencyKey = "key-$i", groupKey = "shed-${i % 5}", createdAt = i.toLong()))
        }
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L })

        val result = engine.drainOnce()

        assertTrue(result) // a clean full drain
        assertEquals(250, api.submitCalls.size) // every row was dispatched exactly once
        // Pulled in MORE THAN ONE fetch, and every fetch was BOUNDED (never one unbounded SELECT *).
        assertTrue("expected multiple batched fetches, got ${store.drainLimits}", store.drainLimits.size >= 2)
        assertTrue("every fetch must pass a finite limit", store.drainLimits.all { it in 1..1_000 })
    }

    @Test
    fun `same-group FIFO is preserved across a batch boundary`() = runBlocking {
        val store = RecordingOutboxStore()
        // One group, more rows than a single batch. Inserted newest-first to prove the drain sorts
        // by createdAt, not insertion order — and that the ordering holds ACROSS the batch split.
        val n = 220
        for (i in (n - 1) downTo 0) {
            store.insert(queuedShedSubmit(id = "row-$i", idempotencyKey = "key-$i", groupKey = "shed-1", createdAt = i.toLong()))
        }
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, key, _ ->
                order += key
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L })

        engine.drainOnce()

        assertTrue("expected the group to span >1 batch", store.drainLimits.size >= 2)
        assertEquals((0 until n).map { "key-$it" }, order) // strict oldest-first, unbroken over the boundary
    }

    @Test
    fun `same-group failure in an over-batch pass blocks newer same-group rows for the rest of the pass`() = runBlocking {
        val store = RecordingOutboxStore()
        // 201 rows in ONE group so the FIRST full batch fills up entirely with this group. The oldest
        // fails; the group's remaining rows in that batch are NOT dispatched (FIFO break). The risk the
        // multi-batch loop introduced: the SECOND fetch re-includes the newer QUEUED same-group rows
        // (the failed oldest is now excluded by its backoff) and would dispatch them ahead of the older
        // failed write — posting newer submissions over an older un-synced one.
        val n = 201
        for (i in 0 until n) {
            store.insert(queuedShedSubmit(id = "row-$i", idempotencyKey = "key-$i", groupKey = "shed-1", createdAt = i.toLong()))
        }
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, key, _ ->
                order += key
                if (key == "key-0") throw IOException("oldest failed") // FIFO head fails
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L }, backoff = BackoffPolicy { 60_000L })

        engine.drainOnce()

        // Only the failed oldest was attempted this pass. No newer same-group row jumped ahead of it,
        // even though the loop ran a second batch fetch (the backed-off head no longer appears there).
        assertEquals(listOf("key-0"), order)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-0")!!.status)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-200")!!.status)
    }

    @Test
    fun `a completed pass returns true even when a row fails, leaving the retry to the scheduler`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("network down") }
        }
        var scheduledAt: Long? = null
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { true },
            clock = { 1_000L },
            backoff = BackoffPolicy { 60_000L },
            retryScheduler = SyncRetryScheduler { scheduledAt = it },
        )

        val result = engine.drainOnce()

        // The row was ATTEMPTED, so the pass COMPLETED -> true. SyncWorker therefore reports
        // success and does NOT also Result.retry(); the failed row's next attempt is owned solely
        // by the explicit retry work booked here. (No worker-retry + explicit-schedule double-drain.)
        assertTrue(result)
        assertEquals(61_000L, scheduledAt)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-1")!!.status)
    }

    @Test
    fun `multiple group failures schedule only the earliest retry for the pass`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(id = "row-1", groupKey = "shed-1", idempotencyKey = "key-1", createdAt = 100L))
        store.insert(
            queuedShedSubmit(id = "row-2", groupKey = "shed-2", idempotencyKey = "key-2", createdAt = 200L)
                .copy(status = OutboxStatus.FAILED.name, attemptCount = 1, nextAttemptAt = 1_000L),
        )
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("network down") }
        }
        val scheduledAt = mutableListOf<Long>()
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { true },
            clock = { 1_000L },
            backoff = BackoffPolicy { attempt -> if (attempt == 1) 60_000L else 300_000L },
            retryScheduler = SyncRetryScheduler { scheduledAt += it },
        )

        engine.drainOnce()

        assertEquals(listOf(61_000L), scheduledAt)
        assertEquals(61_000L, store.findById("row-1")!!.nextAttemptAt)
        assertEquals(301_000L, store.findById("row-2")!!.nextAttemptAt)
    }

    @Test
    fun `an offline pass returns false and schedules nothing so the worker reruns it`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        var scheduledAt: Long? = null
        val api = ScriptedAppApi()
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { false },
            clock = { 0L },
            retryScheduler = SyncRetryScheduler { scheduledAt = it },
        )

        val result = engine.drainOnce()

        assertTrue(!result) // aborted before attempting -> worker returns Result.retry()
        assertEquals(null, scheduledAt) // nothing attempted -> nothing to back off
        assertEquals(0, api.submitCalls.size)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-1")!!.status)
    }

    @Test
    fun `a clean full drain returns true`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        assertTrue(engine.drainOnce())
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-1")!!.status)
    }
}

/**
 * Wraps [FakeOutboxStore] to record every eligibility fetch's [limit] so a test can assert the
 * drain pulls rows in bounded batches (see the batching tests) rather than one unbounded SELECT.
 * Everything else delegates unchanged.
 */
private class RecordingOutboxStore(private val inner: FakeOutboxStore = FakeOutboxStore()) : OutboxStore {
    val drainLimits = mutableListOf<Int>()

    override suspend fun insert(entity: OutboxEntity) = inner.insert(entity)
    override suspend fun findById(id: String) = inner.findById(id)
    override suspend fun findByIdempotencyKey(key: String) = inner.findByIdempotencyKey(key)

    override suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity> {
        drainLimits += limit
        return inner.eligibleForDrain(now, limit)
    }

    override fun observeAll() = inner.observeAll()
    override suspend fun markInFlight(id: String, now: Long) = inner.markInFlight(id, now)
    override suspend fun markSucceeded(id: String, resultJson: String, now: Long) = inner.markSucceeded(id, resultJson, now)
    override suspend fun markFailed(id: String, attemptCount: Int, nextAttemptAt: Long, conflict: Boolean, lastError: String, now: Long) =
        inner.markFailed(id, attemptCount, nextAttemptAt, conflict, lastError, now)
    override suspend fun markRetryReady(id: String, now: Long) = inner.markRetryReady(id, now)
    override suspend fun reclaimInFlight(now: Long) = inner.reclaimInFlight(now)
}
