package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.delay
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
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.feed.FeedStatus

/**
 * Feed-submit-hardening, round 2: FeedTransportCaptureViewModel had NO already-submitted gate at
 * all before this — it derived `submitEnabled` purely from local proof/draft state, so a reinstall
 * or a status change while the screen was open re-offered a full capture form for a task already
 * with the verifier (the same class of bug as packing/distribution, STG 2026-08-09). This adds a
 * nav-arg hint (first paint) superseded by a live Room-backed observer, mirroring the other two
 * completion screens.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedTransportCaptureLiveStatusTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun savedState(lifecycleStatus: String) = SavedStateHandle(
        mapOf(
            "task_id" to "task-1",
            "shed_id" to "shed-1",
            "shed_label" to "Shed 1",
            "park_label" to "Farm 1",
            "lifecycle_status" to lifecycleStatus,
        ),
    )

    private fun buildViewModel(feedTransportRepository: FakeFeedTransportStatusSource, lifecycleStatus: String) =
        FeedTransportCaptureViewModel(
            sync = NoopFeedTransportSyncRepository(),
            capture = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            drafts = InMemoryCaptureDraftRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            feedTransportRepository = feedTransportRepository,
            saved = savedState(lifecycleStatus),
        )

    /** REINSTALL CASE — see FeedPackingCompleteLiveStatusTest's identical-shaped test. */
    @Test
    fun `stale open nav-arg hint is overridden by an already-submitted live status`() = runTest(dispatcher) {
        val transportRepository = FakeFeedTransportStatusSource()
        transportRepository.emit(FeedStatus.AWAITING)

        val viewModel = buildViewModel(transportRepository, lifecycleStatus = "open")
        advanceUntilIdle()

        assertTrue(
            "a submitted live status must win over a stale 'open' nav-arg hint",
            viewModel.state.value.alreadySubmitted,
        )
        assertFalse(viewModel.state.value.captureEnabled)
        assertFalse(viewModel.state.value.submitEnabled)
    }

    /** LIVE FLIP WHILE OPEN — see FeedPackingCompleteLiveStatusTest's identical-shaped test. */
    @Test
    fun `live status flips to read-only while the screen stays open`() = runTest(dispatcher) {
        val transportRepository = FakeFeedTransportStatusSource()
        transportRepository.emit(FeedStatus.PENDING)

        val viewModel = buildViewModel(transportRepository, lifecycleStatus = "open")
        advanceUntilIdle()
        assertFalse(viewModel.state.value.alreadySubmitted)

        transportRepository.emit(FeedStatus.AWAITING)
        advanceUntilIdle()

        assertTrue(
            "a live status flip while the screen is open must flip the screen to read-only",
            viewModel.state.value.alreadySubmitted,
        )
        assertFalse(viewModel.state.value.captureEnabled)
    }

    /** OFFLINE-FIRST FALLBACK — see FeedPackingCompleteLiveStatusTest's identical-shaped test. */
    @Test
    fun `no cached room row keeps the nav-arg hint instead of forcing editable`() = runTest(dispatcher) {
        val transportRepository = FakeFeedTransportStatusSource() // never emits a non-null status

        val viewModel = buildViewModel(transportRepository, lifecycleStatus = FeedStatus.COMPLETED)
        advanceUntilIdle()

        assertTrue(
            "with no cached Room row, the nav-arg hint must still be honoured",
            viewModel.state.value.alreadySubmitted,
        )
    }

    /**
     * EDITABLE-FLASH: between construction and the delayed Room emission, the intermediate state
     * must correctly reflect the nav-arg hint — not flip erroneously editable.
     */
    @Test
    fun `delayed live status emission never permits an editable-flash window`() = runTest(dispatcher) {
        val transportRepository = FakeFeedTransportStatusSource()
        // Emit the SUBMITTED status, but delay it past construction so construction sees no value
        transportRepository.emitWithDelay(FeedStatus.AWAITING, delayMs = 1)

        val viewModel = buildViewModel(transportRepository, lifecycleStatus = "open")
        // Before advanceUntilIdle, check intermediate state
        assertFalse(
            "before delayed emission, nav-arg 'open' should be honoured",
            viewModel.state.value.alreadySubmitted,
        )

        advanceUntilIdle()
        // After the delayed emission, live status must override nav-arg
        assertTrue(
            "after delayed emission arrives, submitted live status must override nav-arg",
            viewModel.state.value.alreadySubmitted,
        )
    }
}

private class NoopFeedTransportSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun enqueueFeedTransportSubmit(groupKey: String, idempotencyKey: String, taskId: String, proofOutboxItemId: String): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-outbox-1")
    override suspend fun enqueueFeedPackingComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, packingProofOutboxItemId: String): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDistributionComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, distributionProofOutboxItemId: String?, feedWeightProofOutboxItemId: String?, waterProofOutboxItemId: String?, feedWeightProofRef: String?, distributionProofRef: String?, waterProofRef: String?): AppResult<String> = error("unused")
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
