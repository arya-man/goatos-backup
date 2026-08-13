package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
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
import sg.mesha.goatos.capture.FakePhotoCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.feed.FeedStatus

/**
 * Feed-submit-hardening, round 2: FeedDistributionCompleteViewModel declared
 * `ARG_LIFECYCLE_STATUS` but never actually READ it — the completion screen had NO already-submitted
 * gate at all (the constant existed, unused). This adds the same live, Room-backed gate
 * FeedPackingCompleteViewModel has, superseding the nav-arg hint the moment Room has a value.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedDistributionCompleteLiveStatusTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun savedState(lifecycleStatus: String) = SavedStateHandle(
        mapOf(
            "park_id" to "park-1",
            "shed_id" to "shed-1",
            "session_no" to "1",
            "workflow" to "normal",
            "target_date" to "2026-08-13",
            "shed_label" to "Shed 1",
            "session_label" to "Session 1",
            "park_label" to "Farm 1",
            "partition_label" to "A",
            "lifecycle_status" to lifecycleStatus,
        ),
    )

    private fun buildViewModel(feedRepository: FakeFeedRepository, lifecycleStatus: String) =
        FeedDistributionCompleteViewModel(
            syncRepository = NoopFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            photoCaptureSource = FakePhotoCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            feedRepository = feedRepository,
            savedStateHandle = savedState(lifecycleStatus),
        )

    /** REINSTALL CASE — see FeedPackingCompleteLiveStatusTest's identical-shaped test. */
    @Test
    fun `stale open nav-arg hint is overridden by an already-submitted live status`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitDirectionStatus(FeedStatus.AWAITING)

        val viewModel = buildViewModel(feedRepository, lifecycleStatus = "open")
        advanceUntilIdle()

        assertTrue(
            "a submitted live status must win over a stale 'open' nav-arg hint",
            viewModel.state.value.alreadySubmitted,
        )
        assertTrue(viewModel.state.value.isFinalSubmitted)
        assertFalse(viewModel.state.value.feedWeightPhotoCaptureEnabled)
    }

    /** LIVE FLIP WHILE OPEN — see FeedPackingCompleteLiveStatusTest's identical-shaped test. */
    @Test
    fun `live status flips to read-only while the screen stays open`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitDirectionStatus(FeedStatus.PENDING)

        val viewModel = buildViewModel(feedRepository, lifecycleStatus = "open")
        advanceUntilIdle()
        assertFalse(viewModel.state.value.alreadySubmitted)

        feedRepository.emitDirectionStatus(FeedStatus.AWAITING)
        advanceUntilIdle()

        assertTrue(
            "a live status flip while the screen is open must flip the screen to read-only",
            viewModel.state.value.alreadySubmitted,
        )
    }

    /** OFFLINE-FIRST FALLBACK — see FeedPackingCompleteLiveStatusTest's identical-shaped test. */
    @Test
    fun `no cached room row keeps the nav-arg hint instead of forcing editable`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository() // never emits a non-null status

        val viewModel = buildViewModel(feedRepository, lifecycleStatus = FeedStatus.COMPLETED)
        advanceUntilIdle()

        assertTrue(
            "with no cached Room row, the nav-arg hint must still be honoured",
            viewModel.state.value.alreadySubmitted,
        )
    }
}

private class NoopFeedDistributionSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
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
    ): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = error("unused")
    override suspend fun enqueueFeedTransportSubmit(groupKey: String, idempotencyKey: String, taskId: String, proofOutboxItemId: String): AppResult<String> = error("unused")
    override suspend fun enqueueFeedPackingComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, packingProofOutboxItemId: String): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDirectionComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, sessionNo: Int, targetDate: String, workflow: String): AppResult<String> = error("unused")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun triggerDrain() = Unit
}
