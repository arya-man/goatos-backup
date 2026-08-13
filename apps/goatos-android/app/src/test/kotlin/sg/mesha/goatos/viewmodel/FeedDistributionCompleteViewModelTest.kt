package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
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
import sg.mesha.goatos.capture.CapturedPhoto
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakePhotoCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.feature.feed.FeedDistributionEvent
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.JsonPrimitive

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class FeedDistributionCompleteViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    // A CANCELLED re-capture must leave the existing proof alone. The old order discarded the row
    // first and only then opened the camera, so cancelling it (or a camera failure, or a black
    // preview) deleted a good proof and left the slot empty -- the operator's "proof disappeared".
    @Test
    fun `a cancelled re-capture keeps the existing proof`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        // ONE photo available: the first capture consumes it, so the retake finds the camera empty
        // and returns null, which is exactly what a cancel looks like to the ViewModel.
        val photoSource = FakePhotoCaptureSource(
            mutableListOf(CapturedPhoto(localUri = "/proof/feed-weight.jpg", capturedAtMs = 3L)),
        )
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = photoSource,
            proofCaptureRepository = proofCaptureRepository,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "1",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "normal",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-12",
                ),
            ),
        )

        viewModel.onEvent(FeedDistributionEvent.TakeFeedWeightPhoto)
        advanceUntilIdle()
        assertEquals("the first capture must record one proof", 1, proofCaptureRepository.captureCalls.size)
        assertEquals(true, viewModel.state.value.feedWeightPhotoCaptured)

        // Retake, and cancel it.
        viewModel.onEvent(FeedDistributionEvent.TakeFeedWeightPhoto)
        advanceUntilIdle()

        assertEquals(
            "a cancelled retake must not write a second proof",
            1,
            proofCaptureRepository.captureCalls.size,
        )
        // Assert the ROW, not the flag. The flag stays true even when the row is gone -- which is
        // why the defect looked fine on screen and the proof was simply missing underneath.
        val survivingRows = proofCaptureRepository.observeProofs("any", null).first()
        assertEquals(
            "the existing proof row must survive a cancelled retake -- discarding it first is what lost it",
            1,
            survivingRows.size,
        )
        assertEquals("/proof/feed-weight.jpg", survivingRows.first().localUri)
    }

    @Test
    fun `completion unlocks after all three proof uploads sync and then submits`() = runTest(dispatcher) {
        val syncRepository = RecordingFeedDistributionSyncRepository()
        val videoSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "/proof/feed.mp4", startedAtMs = 1L, endedAtMs = 2L),
                CapturedVideo(localUri = "/proof/water.mp4", startedAtMs = 3L, endedAtMs = 4L),
            ),
        )
        val photoSource = FakePhotoCaptureSource(
            mutableListOf(
                CapturedPhoto(localUri = "/proof/feed-weight.jpg", capturedAtMs = 3L),
            ),
        )
        val proofCaptureRepository = FakeProofCaptureRepository()
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = syncRepository,
            proofCaptureSource = videoSource,
            photoCaptureSource = photoSource,
            proofCaptureRepository = proofCaptureRepository,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "1",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "normal",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-12",
                ),
            ),
        )

        viewModel.onEvent(FeedDistributionEvent.TakeFeedWeightPhoto)
        advanceUntilIdle()
        assertEquals(0, syncRepository.completionEnqueueCount)

        viewModel.onEvent(FeedDistributionEvent.RecordFeedVideo)
        advanceUntilIdle()
        assertEquals(0, syncRepository.completionEnqueueCount)

        viewModel.onEvent(FeedDistributionEvent.RecordWaterVideo)
        advanceUntilIdle()
        assertEquals(0, syncRepository.completionEnqueueCount)
        assertEquals(true, viewModel.state.value.submitEnabled)

        syncRepository.setItemStatus("proof-outbox-1", SyncItemStatus.SUCCEEDED)
        syncRepository.setItemStatus("proof-outbox-2", SyncItemStatus.SUCCEEDED)
        syncRepository.setItemStatus("proof-outbox-3", SyncItemStatus.SUCCEEDED)
        advanceUntilIdle()
        assertEquals(FeedDistributionProofStatus.SYNCED, viewModel.state.value.feedWeightPhotoStatus)
        assertEquals(true, viewModel.state.value.submitEnabled)

        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        advanceUntilIdle()
        assertEquals(1, syncRepository.completionEnqueueCount)

        assertEquals(
            listOf("/proof/feed-weight.jpg", "/proof/feed.mp4", "/proof/water.mp4"),
            proofCaptureRepository.captureCalls.map { it.localUri },
        )
        assertEquals("proof-outbox-1", syncRepository.lastFeedWeightProofOutboxItemId)
        assertEquals("proof-outbox-2", syncRepository.lastDistributionProofOutboxItemId)
        assertEquals("proof-outbox-3", syncRepository.lastWaterProofOutboxItemId)
    }

    /**
     * Re-entering the screen on a PARTITIONED pen must rehydrate an already-captured proof from
     * Room. The view model is scoped to its nav back stack entry, so leaving the screen clears all
     * in-memory state (and the SavedStateHandle with it) — the durable proof read is the only thing
     * that can restore the capture, and it was silently returning nothing.
     *
     * The write stores partitionKey "whole" (the feed capture calls pass no label), while the read
     * used to pass the pen label and therefore asked for "3". Empty result, so the operator came
     * back to a blank form for a video already recorded and uploading. It only ever worked on an
     * UNDIVIDED shed, where both sides collapse to "whole" — which is why the case above passes
     * and this one did not.
     */
    @Test
    fun `captured proof rehydrates when the screen is reopened on a partitioned pen`() = runTest(dispatcher) {
        val syncRepository = RecordingFeedDistributionSyncRepository()
        // ONE repository across both view models: the durable Room-backed store that survives the
        // screen being popped off the back stack.
        val proofCaptureRepository = FakeProofCaptureRepository()

        fun viewModelForPen() = FeedDistributionCompleteViewModel(
            syncRepository = syncRepository,
            proofCaptureSource = FakeProofCaptureSource(
                mutableListOf(CapturedVideo(localUri = "/proof/feed.mp4", startedAtMs = 1L, endedAtMs = 2L)),
            ),
            photoCaptureSource = FakePhotoCaptureSource(
                mutableListOf(CapturedPhoto(localUri = "/proof/feed-weight.jpg", capturedAtMs = 3L)),
            ),
            proofCaptureRepository = proofCaptureRepository,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf<String, Any?>(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "1",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "normal",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-12",
                    // Stale parent-shed payloads need the hidden compatibility discriminator.
                    FeedDistributionCompleteViewModel.ARG_LOCATION_IDENTITY_KEY to "shed-1|part-3",
                    FeedDistributionCompleteViewModel.ARG_COMPAT_PARTITION_LABEL to "Part 3",
                ),
            ),
        )

        val first = viewModelForPen()
        first.onEvent(FeedDistributionEvent.RecordFeedVideo)
        advanceUntilIdle()
        assertEquals(true, first.state.value.videoCaptured)

        // Back navigation pops the entry and clears the view model; reopening builds a fresh one
        // against the same durable store.
        val reopened = viewModelForPen()
        advanceUntilIdle()

        assertEquals(true, reopened.state.value.videoCaptured)
        assertEquals("/proof/feed.mp4", reopened.state.value.videoPreviewPath)
    }
}

private class RecordingFeedDistributionSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()
    val proofTypes = mutableListOf<String>()
    val proofMetadata = mutableListOf<Map<String, kotlinx.serialization.json.JsonElement>>()
    var completionEnqueueCount = 0
        private set
    var lastDistributionProofOutboxItemId: String? = null
        private set
    var lastFeedWeightProofOutboxItemId: String? = null
        private set
    var lastWaterProofOutboxItemId: String? = null
        private set

    override fun observeStatus(): MutableStateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = items.getOrPut(itemId) { MutableStateFlow(null) }

    fun setItemStatus(itemId: String, itemStatus: SyncItemStatus) {
        items.getOrPut(itemId) { MutableStateFlow(null) }.value = SyncQueueItem(
            id = itemId,
            opType = "PROOF_UPLOAD",
            idempotencyKey = "proof-key-$itemId",
            groupKey = "feed-dist:shed-1:1:normal",
            status = itemStatus,
            attemptCount = 0,
            maxAttempts = 3,
            conflict = false,
            createdAt = 1L,
            updatedAt = 2L,
            lastError = null,
        )
    }

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> {
        proofTypes += request.proofType
        proofMetadata += request.metadata
        val itemId = "proof-item-${proofTypes.size}"
        setItemStatus(itemId, SyncItemStatus.QUEUED)
        return AppResult.Ok(itemId)
    }

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
        completionEnqueueCount += 1
        lastFeedWeightProofOutboxItemId = feedWeightProofOutboxItemId
        lastDistributionProofOutboxItemId = distributionProofOutboxItemId
        lastWaterProofOutboxItemId = waterProofOutboxItemId
        return AppResult.Ok("completion-item-1")
    }

    override suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueReschedule(
        obligationId: String,
        groupKey: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueVerificationVerdict(
        itemId: String,
        decision: String,
        reason: String?,
        rowVersion: Int,
    ): AppResult<String> = error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun triggerDrain() = Unit
}
