package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.delay
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
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
        assertFalse(viewModel.state.value.videoCaptureEnabled)
        assertFalse(viewModel.state.value.waterVideoCaptureEnabled)
        assertFalse(viewModel.state.value.submitEnabled)
    }

    /**
     * Blocker 7: the computed flags above are necessary but not sufficient — this proves the
     * gate actually BLOCKS the interaction paths (the action functions behind the buttons), not
     * just the displayed/derived state. A stale "open" nav-arg with a live "submitted" backend
     * status must make a capture tap AND a mark-done/submit tap both no-ops.
     */
    @Test
    fun `capture and submit taps are no-ops when a stale open nav-arg is overridden by a submitted live status`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitDirectionStatus(FeedStatus.AWAITING)
        val proofCaptureSource = FakeProofCaptureSource()
        val photoCaptureSource = FakePhotoCaptureSource()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val sync = CountingFeedDistributionSyncRepository()
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = sync,
            proofCaptureSource = proofCaptureSource,
            photoCaptureSource = photoCaptureSource,
            proofCaptureRepository = proofCaptureRepository,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            feedRepository = feedRepository,
            savedStateHandle = savedState("open"),
        )
        advanceUntilIdle()
        assertTrue(viewModel.state.value.isFinalSubmitted)

        viewModel.onEvent(sg.mesha.goatos.feature.feed.FeedDistributionEvent.TakeFeedWeightPhoto)
        viewModel.onEvent(sg.mesha.goatos.feature.feed.FeedDistributionEvent.RecordFeedVideo)
        viewModel.onEvent(sg.mesha.goatos.feature.feed.FeedDistributionEvent.RecordWaterVideo)
        viewModel.onEvent(sg.mesha.goatos.feature.feed.FeedDistributionEvent.MarkDone)
        advanceUntilIdle()

        assertEquals(
            "a capture tap must be a no-op once the live status is submitted",
            0,
            proofCaptureSource.captureCount,
        )
        assertEquals(
            "a photo capture tap must be a no-op once the live status is submitted",
            0,
            photoCaptureSource.captureCount,
        )
        assertEquals(
            "a mark-done/submit tap must be a no-op once the live status is submitted",
            0,
            sync.submitEnqueueCalls,
        )
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

    /**
     * EDITABLE-FLASH: between construction and the delayed Room emission, the intermediate state
     * must correctly reflect the nav-arg hint — not flip erroneously editable.
     */
    @Test
    fun `delayed live status emission never permits an editable-flash window`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        // Emit the SUBMITTED status, but delay it past construction so construction sees no value
        feedRepository.emitDirectionStatusWithDelay(FeedStatus.AWAITING, delayMs = 1)

        val viewModel = buildViewModel(feedRepository, lifecycleStatus = "open")
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

    /**
     * SERVER-POLL FLIP: a TEAMMATE submits this same session on another phone. This phone's own
     * Room cache never changes (nothing here synced a fresh worklist page), so
     * [FakeFeedRepository.observeDirectionSessionStatus] alone would keep emitting the stale open
     * status forever. The periodic SERVER poll (fetchDirectionSessionStatus) is what closes that
     * gap: on its next tick it must flip the screen read-only without a back/reopen.
     */
    @Test
    fun `screen open editable flips read-only when the periodic server poll reports pending verification`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitDirectionStatus(FeedStatus.PENDING) // Room cache: still open.

        // runCurrent() (NOT advanceUntilIdle()): the bounded server-poll loop below is finite
        // (MAX_SERVER_STATUS_POLLS) but still spans ~24h of virtual time, and advanceUntilIdle()
        // would greedily run every one of those ticks right here, exhausting the poll budget
        // before this test gets to control it. runCurrent() only drains work that is ALREADY ready
        // (the Room-backed flow's synchronous first emission), leaving the delayed poll tick alone.
        val viewModel = buildViewModel(feedRepository, lifecycleStatus = "open")
        runCurrent()
        assertFalse("screen starts editable", viewModel.state.value.alreadySubmitted)

        // The Room-backed source never changes; only the SERVER poll learns about the teammate's
        // submit.
        feedRepository.queueServerDirectionStatus(FeedStatus.AWAITING)
        advanceTimeBy(30_001L)

        assertTrue(
            "a server-poll tick reporting pending_verification must flip the screen read-only",
            viewModel.state.value.alreadySubmitted,
        )
        assertFalse(viewModel.state.value.videoCaptureEnabled)
        assertFalse(viewModel.state.value.submitEnabled)
    }

    /**
     * FAIL-SOFT: a poll failure (offline/timeout/5xx) must leave the screen exactly as it was —
     * never flips editable -> locked on a guess.
     */
    @Test
    fun `a failed server poll tick leaves the editable state unchanged`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitDirectionStatus(FeedStatus.PENDING)

        val viewModel = buildViewModel(feedRepository, lifecycleStatus = "open")
        runCurrent() // see the sibling test above for why this is runCurrent(), not advanceUntilIdle().
        assertFalse(viewModel.state.value.alreadySubmitted)

        feedRepository.queueServerDirectionFailure()
        advanceTimeBy(30_001L)

        assertFalse(
            "a failed poll tick must never flip the screen to read-only",
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
        distributionProofOutboxItemId: String?,
        feedWeightProofOutboxItemId: String?,
        waterProofOutboxItemId: String?,
        feedWeightProofRef: String?,
        distributionProofRef: String?,
        waterProofRef: String?,
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

/** Counts [enqueueFeedDistributionComplete] calls; everything else is unused/no-op. Used to prove
 *  a mark-done/submit tap is a structural no-op once the live status is already submitted
 *  (blocker 7), not merely hidden behind a disabled-looking button. */
private class CountingFeedDistributionSyncRepository : SyncRepository {
    var submitEnqueueCalls: Int = 0
        private set
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
        distributionProofOutboxItemId: String?,
        feedWeightProofOutboxItemId: String?,
        waterProofOutboxItemId: String?,
        feedWeightProofRef: String?,
        distributionProofRef: String?,
        waterProofRef: String?,
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
