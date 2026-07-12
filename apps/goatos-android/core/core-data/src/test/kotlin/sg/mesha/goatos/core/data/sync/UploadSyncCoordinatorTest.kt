package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import java.io.IOException

/**
 * Deterministic dispatchers on the `runTest` scheduler. [SyncEngine.drainOnce] `launch`es one
 * coroutine per outbox group on `dispatchers.io`; backed by the real multi-threaded `Dispatchers.IO`
 * those groups race, so which snapshot a pass observed (and thus which sub-test flaked) varied run to
 * run. A single [StandardTestDispatcher] serializes every launch in FIFO order — deterministic and
 * faithful to real suspend/resume ordering.
 */
private fun TestScope.testDispatchers(): DispatcherProvider {
    val dispatcher = StandardTestDispatcher(testScheduler)
    return object : DispatcherProvider {
        override val io = dispatcher
        override val default = dispatcher
        override val main = dispatcher
    }
}

/**
 * Coverage for the drain loop behind the background upload foreground service
 * (`sg.mesha.goatos.sync.UploadForegroundService`, `:app`) — [UploadSyncCoordinator] IS that
 * service's entire "do the work" body, so this is the real proof of its resume/progress/stop
 * behavior, on the plain JVM with [FakeOutboxStore] + [ScriptedAppApi] (same doubles
 * [SyncEngineTest] uses — no Room/Robolectric needed for THIS layer; the durable-across-a-
 * simulated-process-restart proof lives in [OutboxProcessRestartResumeTest], which uses a real
 * file-backed Room database).
 */
class UploadSyncCoordinatorTest {

    private fun proofUploadRow(
        id: String = "row-1",
        groupKey: String = "shed-1",
        idempotencyKey: String = "proof-key-1",
        status: OutboxStatus = OutboxStatus.QUEUED,
        createdAt: Long = 0L,
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.PROOF_UPLOAD.name,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ProofUploadPayload(request = ProofUploadRequestDto(scopeType = "shed", scopeId = groupKey, subjectType = "shed")),
        ),
        status = status.name,
        attemptCount = 0,
        maxAttempts = DEFAULT_MAX_ATTEMPTS,
        conflict = false,
        createdAt = createdAt,
        updatedAt = createdAt,
        nextAttemptAt = createdAt,
        lastError = null,
        resultJson = null,
    )

    private fun verifyTaskRow(id: String, idempotencyKey: String) = OutboxEntity(
        id = id,
        opType = OutboxOpType.VERIFY_TASK.name,
        groupKey = "task-1",
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            VerifyTaskPayload(taskId = "task-1", request = ReviewTaskRequestDto(reason = "ok", rowVersion = 1)),
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
    )

    @Test
    fun `nothing queued reports Idle immediately without touching the engine`() = runTest {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        val coordinator = UploadSyncCoordinator(SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, dispatchers = testDispatchers()), store)
        val progress = mutableListOf<Pair<Int, Int>>()

        val outcome = coordinator.run { done, total -> progress += done to total }

        assertEquals(UploadSyncCoordinator.Outcome.Idle, outcome)
        assertEquals(listOf(0 to 0), progress)
        assertEquals(0, api.submitCalls.size)
    }

    @Test
    fun `drains every queued proof upload and reports Idle when the relevant queue empties`() = runTest {
        val store = FakeOutboxStore()
        store.insert(proofUploadRow(id = "row-1", idempotencyKey = "key-1"))
        store.insert(proofUploadRow(id = "row-2", groupKey = "shed-2", idempotencyKey = "key-2"))
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, dispatchers = testDispatchers())
        val coordinator = UploadSyncCoordinator(engine, store)
        val progress = mutableListOf<Pair<Int, Int>>()

        val outcome = coordinator.run { done, total -> progress += done to total }

        assertEquals(UploadSyncCoordinator.Outcome.Idle, outcome)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-2")!!.status)
        // First callback is the pre-drain snapshot (2 total, 0 done); the final callback shows
        // every relevant row done.
        assertEquals(2 to 2, progress.first())
        assertEquals(2 to 2, progress.last())
    }

    @Test
    fun `ignores non-upload op types entirely (VERIFY_TASK never starts a drain pass)`() = runTest {
        val store = FakeOutboxStore()
        store.insert(verifyTaskRow(id = "row-v1", idempotencyKey = "verify-key-1"))
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, dispatchers = testDispatchers())
        val coordinator = UploadSyncCoordinator(engine, store)

        val outcome = coordinator.run()

        assertEquals(UploadSyncCoordinator.Outcome.Idle, outcome)
        // The row is untouched by the coordinator — still QUEUED, no attempt spent. It still
        // drains via the ordinary triggerDrainAsync()/WorkManager path, just not through here.
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-v1")!!.status)
    }

    @Test
    fun `offline reports Waiting on the first pass without spending an attempt`() = runTest {
        val store = FakeOutboxStore()
        store.insert(proofUploadRow())
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { false }, clock = { 0L }, dispatchers = testDispatchers())
        val coordinator = UploadSyncCoordinator(engine, store)

        val outcome = coordinator.run()

        assertEquals(UploadSyncCoordinator.Outcome.Waiting(1), outcome)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-1")!!.status)
        assertEquals(0, api.submitCalls.size)
    }

    @Test
    fun `a transport failure that backs off reports Waiting instead of spinning`() = runTest {
        val store = FakeOutboxStore()
        store.insert(proofUploadRow())
        val api = ScriptedAppApi().apply {
            registerProofFn = { _, _ -> throw IOException("network down") }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, backoff = BackoffPolicy { 60_000L }, dispatchers = testDispatchers())
        val coordinator = UploadSyncCoordinator(engine, store)
        var passes = 0
        val outcome = coordinator.run { _, _ -> passes++ }

        assertTrue(outcome is UploadSyncCoordinator.Outcome.Waiting)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-1")!!.status)
        // Exactly ONE drain pass ran before the coordinator recognized the stall and stopped —
        // it never spun waiting for a backoff window that only a future trigger can satisfy.
        assertEquals(2, passes) // pre-drain snapshot callback + the one post-pass callback
    }

    @Test
    fun `a reclaimed stranded IN_FLIGHT row still drains through the coordinator`() = runTest {
        val store = FakeOutboxStore()
        store.insert(proofUploadRow(idempotencyKey = "stranded-key"))
        // Simulate a kill mid-upload: the row was marked IN_FLIGHT, then the process died before
        // markSucceeded/markFailed ran.
        store.markInFlight("row-1", now = 5L)
        assertEquals(OutboxStatus.IN_FLIGHT.name, store.findById("row-1")!!.status)
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 10L }, dispatchers = testDispatchers())
        val coordinator = UploadSyncCoordinator(engine, store)

        val outcome = coordinator.run()

        assertEquals(UploadSyncCoordinator.Outcome.Idle, outcome)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-1")!!.status)
    }

    @Test
    fun `total high-water-marks across the run so a mid-run enqueue never regresses the fraction`() = runTest {
        val store = FakeOutboxStore()
        store.insert(proofUploadRow(id = "row-1", idempotencyKey = "key-1", createdAt = 0L))
        val api = ScriptedAppApi()
        var addedSecond = false
        api.registerProofFn = { key, request ->
            // While the FIRST row is dispatching, a second video finishes capture and gets
            // enqueued concurrently (mirrors a real capture racing the upload of an earlier one).
            // SyncEngine's own batch-fetch semantics (a fresh eligibleForDrain snapshot is only
            // re-read once the CURRENT batch is exhausted) mean this new row is NOT swept into
            // the SAME drainOnce() pass that is currently dispatching row-1 — it surfaces on the
            // coordinator's NEXT pass, exactly like a real capture finishing a beat after the
            // previous upload's network call already started.
            if (!addedSecond) {
                addedSecond = true
                store.insert(proofUploadRow(id = "row-2", groupKey = "shed-2", idempotencyKey = "key-2", createdAt = 1L))
            }
            // Mirror FakeAppApi.registerProof — a VALID envelope with a non-blank proofId. An empty
            // ProofUploadResponseDto() (blank proofId) makes dispatchProofUpload throw
            // NonRetryableSyncException("Proof registration did not return a proof id."), which is what
            // made this row terminal-FAILED; the empty DTO predated the two-step register→upload
            // dispatch and was invalid for it. The side effect above (enqueue row-2 mid-dispatch) is
            // the only thing this hook actually needs to script.
            ProofUploadResponseDto(
                proof = ProofReferenceDto(
                    proofId = "fake-proof-$key",
                    proofType = request.proofType,
                    subjectType = request.subjectType,
                    subjectId = request.subjectId,
                    uploadState = "pending",
                ),
                uploadUrl = "https://fake.local/proofs/upload",
                uploadMethod = "PUT",
            )
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, dispatchers = testDispatchers())
        val coordinator = UploadSyncCoordinator(engine, store)
        val progress = mutableListOf<Pair<Int, Int>>()

        val outcome = coordinator.run { done, total -> progress += done to total }

        assertEquals(UploadSyncCoordinator.Outcome.Idle, outcome)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-2")!!.status)
        // total only ever grows or holds — never regresses below a previously reported value,
        // even though row-2 was enqueued mid-run, after the coordinator's first progress report.
        for (i in 1 until progress.size) {
            assertTrue("total regressed: ${progress[i - 1]} -> ${progress[i]}", progress[i].second >= progress[i - 1].second)
        }
        // Both rows finished; the coordinator's LAST report is a fully-done snapshot.
        assertEquals(progress.last().second, progress.last().first)
        assertTrue("expected row-2 to have been observed at some point", progress.any { it.second >= 1 })
    }
}
