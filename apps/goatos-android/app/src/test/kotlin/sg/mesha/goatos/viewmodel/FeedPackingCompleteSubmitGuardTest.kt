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
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.feature.feed.FeedPackingCompleteEvent

/**
 * Issue 1 (submit hardening): FeedPackingCompleteViewModel.markDone() had NO in-flight/double-tap
 * latch — it relied only on UI canComplete check, so a fast double-tap could call
 * [SyncRepository.enqueueFeedPackingComplete] twice before the first enqueue's state update
 * landed. RED before the fix: two enqueue calls for two rapid MarkDone events.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedPackingCompleteSubmitGuardTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `double-tap markDone enqueues exactly once`() = runTest(dispatcher) {
        val sync = CountingFeedPackingCompleteSyncRepository()
        val drafts = InMemoryCaptureDraftRepository()

        // Set up a proof as if it's already captured (simulates successful RecordPackingVideo)
        val proofOutboxId = "packing-proof-1"
        val groupKey = "feed-pack:2026-08-13:shed-1:a:1:feed" // feedCaptureGroupKey("feed-pack", shed-1, A, 1, feed, 2026-08-13)
        drafts.putProof(CaptureFlow.FEED_PACKING, groupKey, "video", proofOutboxId)

        val saved = SavedStateHandle(
            mapOf(
                "shed_id" to "shed-1",
                "session_no" to "1",
                "workflow" to "feed",
                "target_date" to "2026-08-13",
                "shed_label" to "Shed 1",
                "session_label" to "Session 1",
                "park_label" to "Farm 1",
                "partition_label" to "A",
                "lifecycle_status" to "open",
            ),
        )
        val viewModel = FeedPackingCompleteViewModel(
            syncRepository = sync,
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            drafts = drafts,
            feedRepository = FakeFeedRepository(),
            savedStateHandle = saved,
        )
        advanceUntilIdle()

        // Mark the proof as ready so canComplete can derive as true
        sync.markProofReady(proofOutboxId)
        advanceUntilIdle()

        // The enqueue call suspends until released, modelling the real gap between a tap landing
        // and the async write actually updating `result`/`canComplete` — the exact window a fast
        // double-tap lands in. A guard keyed only on _state.value (read before the launch) would
        // let BOTH taps observe the still-open gate and enqueue twice.
        val gate = CompletableDeferred<Unit>()
        sync.holdNextEnqueueUntil(gate)
        viewModel.onEvent(FeedPackingCompleteEvent.MarkDone)
        viewModel.onEvent(FeedPackingCompleteEvent.MarkDone)
        gate.complete(Unit)
        advanceUntilIdle()

        assertEquals(
            "a double-tap on MarkDone must enqueue exactly one feed-packing-complete write",
            1,
            sync.markDoneEnqueueCalls,
        )
    }

    @Test
    fun `failed markDone resets latch so retry can proceed`() = runTest(dispatcher) {
        val sync = CountingFeedPackingCompleteSyncRepository()
        sync.failNext = true
        val drafts = InMemoryCaptureDraftRepository()

        // Set up a proof as if it's already captured
        val proofOutboxId = "packing-proof-1"
        val groupKey = "feed-pack:2026-08-13:shed-1:a:1:feed" // feedCaptureGroupKey("feed-pack", shed-1, A, 1, feed, 2026-08-13)
        drafts.putProof(CaptureFlow.FEED_PACKING, groupKey, "video", proofOutboxId)

        val saved = SavedStateHandle(
            mapOf(
                "shed_id" to "shed-1",
                "session_no" to "1",
                "workflow" to "feed",
                "target_date" to "2026-08-13",
                "shed_label" to "Shed 1",
                "session_label" to "Session 1",
                "park_label" to "Farm 1",
                "partition_label" to "A",
                "lifecycle_status" to "open",
            ),
        )
        val viewModel = FeedPackingCompleteViewModel(
            syncRepository = sync,
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            drafts = drafts,
            feedRepository = FakeFeedRepository(),
            savedStateHandle = saved,
        )
        advanceUntilIdle()

        // Mark the proof as ready
        sync.markProofReady(proofOutboxId)
        advanceUntilIdle()

        // First attempt fails
        viewModel.onEvent(FeedPackingCompleteEvent.MarkDone)
        advanceUntilIdle()
        assertEquals("first attempt should fail", 1, sync.markDoneEnqueueCalls)

        // Reset for retry
        sync.failNext = false
        // Retry should succeed (latch was reset on the first failure)
        viewModel.onEvent(FeedPackingCompleteEvent.MarkDone)
        advanceUntilIdle()
        assertEquals("retry after failure should succeed", 2, sync.markDoneEnqueueCalls)
    }
}

/** Counts [SyncRepository.enqueueFeedPackingComplete] calls; everything else is unused/no-op. */
private class CountingFeedPackingCompleteSyncRepository : SyncRepository {
    var markDoneEnqueueCalls: Int = 0
        private set
    var failNext: Boolean = false
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    // REACTIVE per-item state, not a one-shot flowOf(): observeItem is called ONCE, while the VM
    // is loading its draft, BEFORE the test calls markProofReady — a one-shot flowOf(null) would
    // complete immediately and the VM's collector would never see the later "ready" transition, so
    // canComplete would never flip true and markDone() would silently no-op (RED for the wrong
    // reason: not the double-tap guard, but a fake that cannot model a status arriving later).
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()
    private var pendingGate: CompletableDeferred<Unit>? = null

    fun markProofReady(proofId: String) {
        items.getOrPut(proofId) { MutableStateFlow(null) }.value = SyncQueueItem(
            id = proofId,
            opType = "test",
            idempotencyKey = "test-key",
            groupKey = "test-group",
            status = sg.mesha.goatos.core.data.sync.SyncItemStatus.QUEUED,
            attemptCount = 0,
            maxAttempts = 3,
            conflict = false,
            createdAt = System.currentTimeMillis(),
            updatedAt = System.currentTimeMillis(),
            lastError = null,
        )
    }

    fun holdNextEnqueueUntil(gate: CompletableDeferred<Unit>) {
        pendingGate = gate
    }

    override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> =
        items.getOrPut(itemId) { MutableStateFlow(null) }

    override suspend fun enqueueFeedPackingComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        packingProofOutboxItemId: String,
    ): AppResult<String> {
        pendingGate?.let { gate -> pendingGate = null; gate.await() }
        markDoneEnqueueCalls += 1
        return if (failNext) {
            AppResult.Err("test error", Exception("test failure"))
        } else {
            AppResult.Ok("complete-outbox-$markDoneEnqueueCalls")
        }
    }

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-outbox-1")

    override suspend fun enqueueFeedTransportSubmit(groupKey: String, idempotencyKey: String, taskId: String, proofOutboxItemId: String): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDistributionComplete(

        groupKey: String,

        idempotencyKey: String,

        parkId: String?,

        shedId: String,

        partitionLabel: String?,

        sessionNo: Int,

        targetDate: String,

        workflow: String,

        distributionProofOutboxItemId: String?,

        feedWeightProofOutboxItemId: String?,

        waterProofOutboxItemId: String?,

        feedWeightProofRef: String?,

        distributionProofRef: String?,

        waterProofRef: String?,

    ): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDirectionComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, sessionNo: Int, targetDate: String, workflow: String): AppResult<String> = error("unused")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
