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
import android.content.Context
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.feature.feed.FeedDistributionEvent

/**
 * Issue 1 (submit hardening): FeedDistributionCompleteViewModel.markDone() had NO red/green test
 * for its in-flight/double-tap latch (completeEnqueueInFlight). The latch exists but was untested.
 * RED before the fix: two enqueue calls for two rapid MarkDone events.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedDistributionCompleteSubmitGuardTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `double-tap markDone enqueues exactly once`() = runTest(dispatcher) {
        val sync = CountingFeedDistributionCompleteSyncRepository()
        val context = RuntimeEnvironment.getApplication()
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
        val feedWeightPhotoProofItemId = DraftOutboxItemId(saved, "feedDistribution.feedWeightPhotoProofItemId")
        val videoProofItemId = DraftOutboxItemId(saved, "feedDistribution.videoProofItemId")
        val waterVideoProofItemId = DraftOutboxItemId(saved, "feedDistribution.waterVideoProofItemId")

        // Pre-set all three proof IDs so canComplete can derive as true
        feedWeightPhotoProofItemId.value = "proof-photo-1"
        videoProofItemId.value = "proof-video-1"
        waterVideoProofItemId.value = "proof-water-1"

        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = sync,
            proofCaptureSource = FakeProofCaptureSource(),
            photoCaptureSource = FakePhotoCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = context,
            feedRepository = FakeFeedRepository(),
            savedStateHandle = saved,
        )
        advanceUntilIdle()

        // Mark all three proofs as ready
        sync.markProofReady("proof-photo-1")
        sync.markProofReady("proof-video-1")
        sync.markProofReady("proof-water-1")
        advanceUntilIdle()

        // The enqueue call suspends until released, modelling the real gap between a tap landing
        // and the async write actually updating state — the exact window a fast double-tap lands in.
        val gate = CompletableDeferred<Unit>()
        sync.holdNextEnqueueUntil(gate)
        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        gate.complete(Unit)
        advanceUntilIdle()

        assertEquals(
            "a double-tap on MarkDone must enqueue exactly one feed-distribution-complete write",
            1,
            sync.markDoneEnqueueCalls,
        )
    }

    @Test
    fun `failed markDone resets latch so retry can proceed`() = runTest(dispatcher) {
        val sync = CountingFeedDistributionCompleteSyncRepository()
        sync.failNext = true
        val context = RuntimeEnvironment.getApplication()
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
        val feedWeightPhotoProofItemId = DraftOutboxItemId(saved, "feedDistribution.feedWeightPhotoProofItemId")
        val videoProofItemId = DraftOutboxItemId(saved, "feedDistribution.videoProofItemId")
        val waterVideoProofItemId = DraftOutboxItemId(saved, "feedDistribution.waterVideoProofItemId")

        // Pre-set all three proof IDs
        feedWeightPhotoProofItemId.value = "proof-photo-1"
        videoProofItemId.value = "proof-video-1"
        waterVideoProofItemId.value = "proof-water-1"

        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = sync,
            proofCaptureSource = FakeProofCaptureSource(),
            photoCaptureSource = FakePhotoCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = context,
            feedRepository = FakeFeedRepository(),
            savedStateHandle = saved,
        )
        advanceUntilIdle()

        // Mark all three proofs as ready
        sync.markProofReady("proof-photo-1")
        sync.markProofReady("proof-video-1")
        sync.markProofReady("proof-water-1")
        advanceUntilIdle()

        // First attempt fails
        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        advanceUntilIdle()
        assertEquals("first attempt should fail", 1, sync.markDoneEnqueueCalls)

        // Reset for retry
        sync.failNext = false
        // Retry should succeed (latch was reset on the first failure)
        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        advanceUntilIdle()
        assertEquals("retry after failure should succeed", 2, sync.markDoneEnqueueCalls)
    }
}

/** Counts [SyncRepository.enqueueFeedDistributionComplete] calls. */
private class CountingFeedDistributionCompleteSyncRepository : SyncRepository {
    var markDoneEnqueueCalls: Int = 0
        private set
    var failNext: Boolean = false
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    // REACTIVE per-item state, not a one-shot flowOf() — see the identical fix + rationale in
    // FeedPackingCompleteSubmitGuardTest's CountingFeedPackingCompleteSyncRepository. A one-shot
    // flow computed at the moment the VM subscribes (before the test's markProofReady calls) can
    // never see the later "ready" transition, so canComplete never flips true and markDone()
    // silently no-ops — a RED result for the wrong reason.
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

    override suspend fun enqueueFeedDistributionComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        distributionProofOutboxItemId: String,
        feedWeightProofOutboxItemId: String,
        waterProofOutboxItemId: String,
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
    override suspend fun enqueueFeedPackingComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, packingProofOutboxItemId: String): AppResult<String> = error("unused")
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

/** Simple fake photo capture source. */
private class FakePhotoCaptureSource : sg.mesha.goatos.capture.PhotoCaptureSource {
    override suspend fun capturePhoto(context: sg.mesha.goatos.capture.PhotoCaptureContext): sg.mesha.goatos.capture.CapturedPhoto {
        return sg.mesha.goatos.capture.CapturedPhoto(localUri = "file:///photo.jpg", capturedAtMs = System.currentTimeMillis())
    }
}
