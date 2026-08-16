package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent

/**
 * Blocker 6: [FeedTransportCaptureViewModel]'s recording path called
 * `capture.captureVideo(...)` WITHOUT a try/catch, unlike distribution/packing's identically
 * shaped capture paths (see [FeedPackingCompleteViewModel.capturePackingVideo] and
 * [FeedDistributionCompleteViewModel]'s capture functions), both of which wrap the call and
 * recover to a retryable state. If the camera call throws here, the ViewModel must:
 *  1. leave any already-committed proof file untouched,
 *  2. reset `isCapturing` back to `false` (a retryable state, not a stuck/wedged UI), and
 *  3. surface a visible, retryable error message.
 *
 * RED before the fix: an uncaught exception from `captureVideo()` propagated out of the
 * `viewModelScope.launch { ... }` coroutine, crashing the collector and leaving `isCapturing`
 * stuck `true` forever (no `finally`/`catch` ever ran to reset it) -- the "wedge".
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedTransportRecaptureWedgeTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `captureVideo throwing resets capturing state and surfaces a retryable error, old proof untouched`() = runTest(dispatcher) {
        val sync = RecordingFeedTransportSyncRepository()
        val drafts = InMemoryCaptureDraftRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val saved = SavedStateHandle(
            mapOf(
                "task_id" to "task-1",
                "shed_id" to "shed-1",
                "shed_label" to "Shed 1",
                "park_label" to "Farm 1",
            ),
        )

        // First, record a real video successfully so there is an existing "old proof" on disk /
        // in the repository that a later failed re-record must not disturb.
        val proofSource = ThrowingAfterFirstProofCaptureSource(
            firstResult = CapturedVideo(localUri = "file:///old-video.mp4", startedAtMs = 0L, endedAtMs = 1_000L),
        )
        val viewModel = FeedTransportCaptureViewModel(
            sync = sync,
            capture = proofSource,
            proofCaptureRepository = proofCaptureRepository,
            drafts = drafts,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            feedTransportRepository = FakeFeedTransportStatusSource(),
            saved = saved,
        )
        advanceUntilIdle()

        viewModel.onEvent(FeedTransportCaptureEvent.RecordVideo)
        advanceUntilIdle()
        assertTrue("first capture must succeed and mark the video captured", viewModel.state.value.videoCaptured)
        assertFalse(viewModel.state.value.isCapturing)
        val proofRowsAfterFirstCapture = proofCaptureRepository.captureCalls.size
        assertEquals(1, proofRowsAfterFirstCapture)
        val oldProofLocalUri = proofCaptureRepository.captureCalls.single().localUri

        // Re-record: the camera call throws this time (e.g. CameraX/hardware failure).
        viewModel.onEvent(FeedTransportCaptureEvent.ReRecordVideo)
        advanceUntilIdle()

        // 1. The OLD proof must remain intact -- captureVideo() throwing happens before any
        //    discard/replace logic runs, so no new capture() call for the replacement should have
        //    landed in the repository, and the original row is untouched.
        assertEquals(
            "a thrown captureVideo() must not touch the previously-committed proof",
            proofRowsAfterFirstCapture,
            proofCaptureRepository.captureCalls.size,
        )
        assertEquals(oldProofLocalUri, proofCaptureRepository.captureCalls.single().localUri)

        // 2. isCapturing must reset to a retryable state, not stay wedged true.
        assertFalse(
            "isCapturing must reset to false after a thrown captureVideo(), not stay wedged",
            viewModel.state.value.isCapturing,
        )
        // videoCaptured must still reflect the (untouched) OLD successful capture.
        assertTrue(viewModel.state.value.videoCaptured)

        // 3. A visible, retryable error must be surfaced.
        assertNotNull(
            "a thrown captureVideo() must surface a visible error message",
            viewModel.state.value.videoMessage,
        )

        // Retry: a fresh RecordVideo/ReRecordVideo after the reset must be able to proceed (not
        // permanently wedged) -- capture is accepted again.
        proofSource.allowNextToSucceed(CapturedVideo(localUri = "file:///retry-video.mp4", startedAtMs = 2_000L, endedAtMs = 3_000L))
        viewModel.onEvent(FeedTransportCaptureEvent.ReRecordVideo)
        advanceUntilIdle()
        assertTrue(
            "capture must be retryable after the reset -- isCapturing must not be stuck",
            viewModel.state.value.videoCaptured,
        )
    }
}

/** Succeeds on the first call, then throws on every subsequent call until [allowNextToSucceed]
 *  queues one more successful result. Models a real intermittent camera/hardware failure on
 *  re-record without needing the full [ProofCaptureSource] fake surface. */
private class ThrowingAfterFirstProofCaptureSource(
    firstResult: CapturedVideo,
) : ProofCaptureSource {
    private var pending: CapturedVideo? = firstResult
    private var callCount = 0

    fun allowNextToSucceed(video: CapturedVideo) {
        pending = video
    }

    override suspend fun captureVideo(captureContext: ProofCaptureContext?): CapturedVideo? {
        callCount++
        val queued = pending
        if (queued != null) {
            pending = null
            return queued
        }
        throw IllegalStateException("simulated camera/hardware failure on call #$callCount")
    }

    override suspend fun pickVideo(): CapturedVideo? = captureVideo()
}

/** Records enqueue calls; everything else is unused/no-op. Mirrors
 *  [CountingFeedTransportSyncRepository] in FeedTransportCaptureSubmitGuardTest.kt. */
private class RecordingFeedTransportSyncRepository : SyncRepository {
    var submitEnqueueCalls: Int = 0
        private set
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)

    override suspend fun enqueueFeedTransportSubmit(
        groupKey: String,
        idempotencyKey: String,
        taskId: String,
        proofOutboxItemId: String,
    ): AppResult<String> {
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

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
