package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent

/**
 * Issue 1 (submit hardening): FeedTransportCaptureViewModel.submit() had NO in-flight/double-tap
 * latch — it relied only on outbox idempotency-key dedup, so a fast double-tap could call
 * [SyncRepository.enqueueFeedTransportSubmit] twice before the first enqueue's state update
 * landed. RED before the fix: two enqueue calls for two rapid Submit events.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedTransportCaptureSubmitGuardTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `double-tap submit enqueues exactly once`() = runTest(dispatcher) {
        val sync = CountingFeedTransportSyncRepository()
        val drafts = InMemoryCaptureDraftRepository()
        val saved = SavedStateHandle(
            mapOf(
                "task_id" to "task-1",
                "shed_id" to "shed-1",
                "shed_label" to "Shed 1",
                "park_label" to "Farm 1",
            ),
        )
        val proofSource = FakeProofCaptureSource()
        proofSource.queue(CapturedVideo(localUri = "file:///video.mp4", startedAtMs = 0L, endedAtMs = 1_000L))
        val viewModel = FeedTransportCaptureViewModel(
            sync = sync,
            capture = proofSource,
            proofCaptureRepository = FakeProofCaptureRepository(),
            drafts = drafts,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            saved = saved,
        )
        advanceUntilIdle()
        // Record the mandatory video for real (through the VM's own capture path) so the submit
        // gate opens exactly the way an operator's tap would open it.
        viewModel.onEvent(FeedTransportCaptureEvent.RecordVideo)
        advanceUntilIdle()

        // The enqueue call suspends until released, modelling the real gap between a tap landing
        // and the async write actually updating `result`/`canSubmit` — the exact window a fast
        // double-tap lands in. A guard keyed only on _state.value (read before the launch) would
        // let BOTH taps observe the still-open gate and enqueue twice.
        val gate = CompletableDeferred<Unit>()
        sync.holdNextEnqueueUntil(gate)
        viewModel.onEvent(FeedTransportCaptureEvent.Submit)
        viewModel.onEvent(FeedTransportCaptureEvent.Submit)
        gate.complete(Unit)
        advanceUntilIdle()

        assertEquals(
            "a double-tap on Submit must enqueue exactly one feed-transport submit write",
            1,
            sync.submitEnqueueCalls,
        )
    }
}

/** Counts [SyncRepository.enqueueFeedTransportSubmit] calls; everything else is unused/no-op. */
private class CountingFeedTransportSyncRepository : SyncRepository {
    var submitEnqueueCalls: Int = 0
        private set
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private var pendingGate: CompletableDeferred<Unit>? = null

    /** The NEXT [enqueueFeedTransportSubmit] call suspends until [gate] completes — models the
     *  real gap between a tap and the async write landing, so a fast second tap can race it. */
    fun holdNextEnqueueUntil(gate: CompletableDeferred<Unit>) {
        pendingGate = gate
    }

    override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)

    override suspend fun enqueueFeedTransportSubmit(
        groupKey: String,
        idempotencyKey: String,
        taskId: String,
        proofOutboxItemId: String,
    ): AppResult<String> {
        pendingGate?.let { gate -> pendingGate = null; gate.await() }
        submitEnqueueCalls += 1
        return AppResult.Ok("submit-outbox-$submitEnqueueCalls")
    }

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-outbox-1")

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

internal class InMemoryCaptureDraftRepository : CaptureDraftRepository {
    private val rows = mutableMapOf<String, CaptureDraft>()

    override suspend fun find(flowKey: String, entityId: String): CaptureDraft = rows[entityId] ?: CaptureDraft()
    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> =
        flowOf(rows[entityId] ?: CaptureDraft())
    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> = flowOf(emptyMap())

    override suspend fun putProof(
        flowKey: String,
        entityId: String,
        step: String,
        outboxItemId: String,
        fingerprint: String?,
    ) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(proofs = current.proofs + (step to outboxItemId), fingerprint = fingerprint ?: current.fingerprint)
    }

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) {
        val current = rows[entityId] ?: return
        rows[entityId] = current.copy(proofs = current.proofs - step)
    }

    override suspend fun putSubmit(flowKey: String, entityId: String, idempotencyKey: String?, outboxItemId: String?) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(submitIdempotencyKey = idempotencyKey, submitOutboxItemId = outboxItemId)
    }

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(answers = answers)
    }

    override suspend fun clear(flowKey: String, entityId: String) {
        rows.remove(entityId)
    }
}
