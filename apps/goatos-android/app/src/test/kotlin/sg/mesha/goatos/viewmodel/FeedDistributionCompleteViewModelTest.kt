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
import kotlinx.serialization.json.JsonPrimitive

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class FeedDistributionCompleteViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

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
        assertEquals(false, viewModel.state.value.submitEnabled)

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
