package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
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
import org.junit.Assert.assertTrue
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
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent

/**
 * Codex P1: [FeedTransportCaptureViewModel] used to delete the old proof BEFORE the new capture
 * was durably saved/enqueued, so a repository failure (or a cancel) after that discard lost the
 * old proof outright. The fix routes re-record through
 * [sg.mesha.goatos.core.data.capture.ProofCaptureRepository.captureReplacingLatest], which only
 * discards the old row(s) AFTER the new capture returns [AppResult.Ok] (Manohar ordering). These
 * tests pin that contract the same way [FeedDistributionCompleteViewModelTest] and
 * [MilkFeedingViewModelTest] pin it for their own capture paths.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedTransportRecaptureOrderingTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun buildViewModel(
        proofCaptureRepository: FakeProofCaptureRepository,
        proofSource: FakeProofCaptureSource,
        drafts: InMemoryCaptureDraftRepository = InMemoryCaptureDraftRepository(),
    ) = FeedTransportCaptureViewModel(
        sync = RecordingFeedTransportOrderingSyncRepository(),
        capture = proofSource,
        proofCaptureRepository = proofCaptureRepository,
        drafts = drafts,
        analytics = RecordingAnalytics(),
        crashReporter = NoopCrashReporter(),
        feedTransportRepository = FakeFeedTransportStatusSource(),
        saved = SavedStateHandle(
            mapOf(
                "task_id" to "task-1",
                "shed_id" to "shed-1",
                "shed_label" to "Shed 1",
                "park_label" to "Farm 1",
            ),
        ),
    )

    @Test
    fun `camera cancel on re-record leaves the old proof untouched`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        // ONE video queued: the first RecordVideo consumes it, so the re-record finds the camera
        // empty and gets null back -- exactly what a cancelled capture looks like.
        val proofSource = FakeProofCaptureSource(
            mutableListOf(CapturedVideo(localUri = "file:///old-video.mp4", startedAtMs = 0L, endedAtMs = 1_000L)),
        )
        val viewModel = buildViewModel(proofCaptureRepository, proofSource)
        advanceUntilIdle()

        viewModel.onEvent(FeedTransportCaptureEvent.RecordVideo)
        advanceUntilIdle()
        assertTrue("first capture must succeed", viewModel.state.value.videoCaptured)
        assertEquals(1, proofCaptureRepository.captureCalls.size)

        viewModel.onEvent(FeedTransportCaptureEvent.ReRecordVideo)
        advanceUntilIdle()

        assertEquals(
            "a cancelled re-record must not write a second proof",
            1,
            proofCaptureRepository.captureCalls.size,
        )
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals(
            "the old proof row must survive a cancelled re-record",
            1,
            survivingRows.size,
        )
        assertEquals("file:///old-video.mp4", survivingRows.first().localUri)
        assertTrue("the old proof must remain reported as captured", viewModel.state.value.videoCaptured)
    }

    @Test
    fun `a failed re-record keeps the old proof visible and submittable`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "file:///old-video.mp4", startedAtMs = 0L, endedAtMs = 1_000L),
                CapturedVideo(localUri = "file:///new-video.mp4", startedAtMs = 2_000L, endedAtMs = 3_000L),
            ),
        )
        val viewModel = buildViewModel(proofCaptureRepository, proofSource)
        advanceUntilIdle()

        viewModel.onEvent(FeedTransportCaptureEvent.RecordVideo)
        advanceUntilIdle()
        assertTrue(viewModel.state.value.videoCaptured)
        assertEquals(1, proofCaptureRepository.captureCalls.size)

        // The re-record's camera call succeeds, but the repository capture/enqueue call itself
        // fails (e.g. a policy cap or a Room write failure) -- the old row must not have been
        // touched first. P1 FIX: this is guaranteed now because captureReplacingLatest only
        // registers a pending retirement on success; a failed call leaves no action registered.
        proofCaptureRepository.failNextCapture = true
        viewModel.onEvent(FeedTransportCaptureEvent.ReRecordVideo)
        advanceUntilIdle()

        assertEquals(
            "a failed re-record must not delete the old proof",
            1,
            proofCaptureRepository.captureCalls.size,
        )
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals(
            "the old proof row must survive a failed re-record",
            1,
            survivingRows.size,
        )
        assertEquals("file:///old-video.mp4", survivingRows.first().localUri)
        // The durable row -- what actually gates re-submit after a process restart -- is the
        // source of truth here, matching MilkFeedingViewModelTest's "assert the row, not the
        // flag" convention: a failed retake's error handling may transiently flip the UI flag,
        // but it must never touch the row backing the still-good old proof.
        assertEquals(
            "no outbox item may be created for the failed replacement",
            proofCaptureRepository.captureCalls.size,
            survivingRows.size,
        )
    }

    @Test
    fun `a successful re-record ends with exactly the new proof active`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "file:///old-video.mp4", startedAtMs = 0L, endedAtMs = 1_000L),
                CapturedVideo(localUri = "file:///new-video.mp4", startedAtMs = 2_000L, endedAtMs = 3_000L),
            ),
        )
        val viewModel = buildViewModel(proofCaptureRepository, proofSource)
        advanceUntilIdle()

        viewModel.onEvent(FeedTransportCaptureEvent.RecordVideo)
        advanceUntilIdle()
        assertEquals(1, proofCaptureRepository.captureCalls.size)

        viewModel.onEvent(FeedTransportCaptureEvent.ReRecordVideo)
        advanceUntilIdle()

        assertEquals(
            "a successful re-record must issue a second capture call",
            2,
            proofCaptureRepository.captureCalls.size,
        )
        // P1 FIX: captureReplacingLatest defers removal of old rows until the new row reaches
        // SYNCED (production behavior). Tests that don't explicitly manage sync state can call
        // driveAllPendingRetirements() to complete the replacement and verify the old row is gone.
        proofCaptureRepository.driveAllPendingRetirements()
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals(
            "exactly the new proof must remain active after a successful re-record",
            1,
            survivingRows.size,
        )
        assertEquals("file:///new-video.mp4", survivingRows.first().localUri)
        assertTrue(viewModel.state.value.videoCaptured)
    }

    @Test
    fun `old row survives while replacement is uploading`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "file:///old-video.mp4", startedAtMs = 0L, endedAtMs = 1_000L),
                CapturedVideo(localUri = "file:///new-video.mp4", startedAtMs = 2_000L, endedAtMs = 3_000L),
            ),
        )
        val viewModel = buildViewModel(proofCaptureRepository, proofSource)
        advanceUntilIdle()

        viewModel.onEvent(FeedTransportCaptureEvent.RecordVideo)
        advanceUntilIdle()
        assertEquals(1, proofCaptureRepository.captureCalls.size)
        val oldProofId = proofCaptureRepository.allRows().first().id

        viewModel.onEvent(FeedTransportCaptureEvent.ReRecordVideo)
        advanceUntilIdle()

        assertEquals(
            "a successful re-record must issue a second capture call",
            2,
            proofCaptureRepository.captureCalls.size,
        )
        val allRows = proofCaptureRepository.allRows()
        assertEquals(
            "REGRESSION: old row must survive after new capture succeeds but before SYNCED",
            2,
            allRows.size,
        )
        assertTrue("old row must be in the list", allRows.any { it.id == oldProofId })

        val newProofId = allRows.first { it.id != oldProofId }.id
        // Simulate the new proof uploading (IN_FLIGHT, but not yet SYNCED with serverProofId).
        proofCaptureRepository.markInFlight(newProofId)
        var rowsAfterInFlight = proofCaptureRepository.allRows()
        assertEquals(
            "old row must still survive while replacement is IN_FLIGHT",
            2,
            rowsAfterInFlight.size,
        )

        // Now simulate the new proof reaching SYNCED — this fires the retirement action.
        proofCaptureRepository.markSyncedAndDriveRetirement(newProofId, "server-proof-id-1")
        val rowsAfterSynced = proofCaptureRepository.allRows()
        assertEquals(
            "old row must be removed only when replacement reaches SYNCED + serverProofId",
            1,
            rowsAfterSynced.size,
        )
        assertEquals(
            "only the new proof survives",
            newProofId,
            rowsAfterSynced.first().id,
        )
    }
}

/** Minimal no-op [SyncRepository] fake for this file's tests -- transport re-record no longer
 *  calls [SyncRepository.deleteOutboxItem] directly (that is now handled durably, inside
 *  [sg.mesha.goatos.core.data.capture.ProofCaptureRepository.captureReplacingLatest]'s remove()
 *  call), so nothing here needs to record deletions the way the pre-fix ordering did. */
private class RecordingFeedTransportOrderingSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)

    override suspend fun enqueueFeedTransportSubmit(
        groupKey: String,
        idempotencyKey: String,
        taskId: String,
        proofOutboxItemId: String,
    ): AppResult<String> = AppResult.Ok("submit-outbox-1")

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
