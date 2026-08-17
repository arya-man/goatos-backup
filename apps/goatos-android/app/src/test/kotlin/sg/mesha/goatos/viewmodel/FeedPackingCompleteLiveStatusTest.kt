package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
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
 * Feed-submit-hardening, round 2: FeedPackingCompleteViewModel snapshotted `lifecycle_status` ONCE
 * from the nav-arg at construction (`alreadySubmitted`, see FeedPackingCompleteViewModel.kt ~L83).
 * Reinstall or a status change while the screen stays open left an editable form for an
 * already-submitted session (STG 2026-08-09). This pins the fix: a LIVE Room-backed observer
 * supersedes the nav-arg hint.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedPackingCompleteLiveStatusTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun savedState(lifecycleStatus: String) = SavedStateHandle(
        mapOf(
            "shed_id" to "shed-1",
            "session_no" to "1",
            "workflow" to "feed",
            "target_date" to "2026-08-13",
            "shed_label" to "Shed 1",
            "session_label" to "Session 1",
            "park_label" to "Farm 1",
            "partition_label" to "A",
            "lifecycle_status" to lifecycleStatus,
        ),
    )

    /**
     * REINSTALL CASE: no local draft AND the stale nav-arg hint says "open" — but the Room-backed
     * live source (server truth, filled by the worklist's RemoteMediator) already reports the
     * session as submitted. Server truth must win over the nav-arg hint, even at construction —
     * this asserts the FIRST emitted state, before any explicit "time passes" step.
     */
    @Test
    fun `stale open nav-arg hint is overridden by an already-submitted live status`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitPackingStatus(FeedStatus.AWAITING)

        val viewModel = FeedPackingCompleteViewModel(
            syncRepository = NoopFeedPackingSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            drafts = InMemoryCaptureDraftRepository(),
            feedRepository = feedRepository,
            feedCompletionStore = sg.mesha.goatos.core.data.FeedCompletionLocalStore(),
            // The nav-arg hint alone says "open" (capturable) — this is exactly the stale-snapshot
            // shape from the STG 2026-08-09 report.
            savedStateHandle = savedState(lifecycleStatus = "open"),
        )
        advanceUntilIdle()

        assertTrue(
            "a submitted live status must win over a stale 'open' nav-arg hint",
            viewModel.state.value.alreadySubmitted,
        )
        assertFalse("capture must be disabled once already-submitted", viewModel.state.value.captureEnabled)
    }

    /**
     * LIVE FLIP WHILE OPEN: the screen opens with an editable ("open") hint AND an initially-open
     * live status — canComplete-adjacent capture is allowed — and then a verifier action (or another
     * device's submit) flips the SAME Room row to submitted while the screen is still on screen. The
     * state must flip to read-only without the operator navigating away and back.
     */
    @Test
    fun `live status flips to read-only while the screen stays open`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitPackingStatus(FeedStatus.PENDING)

        val viewModel = FeedPackingCompleteViewModel(
            syncRepository = NoopFeedPackingSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            drafts = InMemoryCaptureDraftRepository(),
            feedRepository = feedRepository,
            feedCompletionStore = sg.mesha.goatos.core.data.FeedCompletionLocalStore(),
            savedStateHandle = savedState(lifecycleStatus = "open"),
        )
        advanceUntilIdle()
        assertFalse("session starts capturable per the live status", viewModel.state.value.alreadySubmitted)

        // The verifier (or another device) decides the session WHILE this screen is open.
        feedRepository.emitPackingStatus(FeedStatus.AWAITING)
        advanceUntilIdle()

        assertTrue(
            "a live status flip while the screen is open must flip the screen to read-only",
            viewModel.state.value.alreadySubmitted,
        )
        assertFalse(viewModel.state.value.captureEnabled)
    }

    /**
     * OFFLINE-FIRST FALLBACK: Room has no cached row yet for this session (fresh install, cold
     * start before the worklist has ever paged it). The `null` emission must NOT force the screen
     * editable OR read-only on its own — it must keep the nav-arg hint.
     */
    @Test
    fun `no cached room row keeps the nav-arg hint instead of forcing editable`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository() // never emits a non-null status

        val viewModel = FeedPackingCompleteViewModel(
            syncRepository = NoopFeedPackingSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            drafts = InMemoryCaptureDraftRepository(),
            feedRepository = feedRepository,
            feedCompletionStore = sg.mesha.goatos.core.data.FeedCompletionLocalStore(),
            // The hint says already-submitted; Room has nothing yet, so the hint must stand.
            savedStateHandle = savedState(lifecycleStatus = FeedStatus.COMPLETED),
        )
        advanceUntilIdle()

        assertTrue(
            "with no cached Room row, the nav-arg hint must still be honoured",
            viewModel.state.value.alreadySubmitted,
        )
    }

    /**
     * EDITABLE-FLASH: between construction and the delayed Room emission, the intermediate state
     * must correctly reflect the nav-arg hint — not flip erroneously editable. This guards against
     * a null-emission window where an early check could see both scopeSubmitted=false (null) and
     * canComplete=true (proof ready) and briefly allow capture before the live status arrives.
     */
    @Test
    fun `delayed live status emission never permits an editable-flash window`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        // Emit the SUBMITTED status, but delay it past construction so construction sees no value
        feedRepository.emitPackingStatusWithDelay(FeedStatus.AWAITING, delayMs = 1)

        val viewModel = FeedPackingCompleteViewModel(
            syncRepository = NoopFeedPackingSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            drafts = InMemoryCaptureDraftRepository(),
            feedRepository = feedRepository,
            feedCompletionStore = sg.mesha.goatos.core.data.FeedCompletionLocalStore(),
            // Nav-arg hint says open, but live status will arrive as submitted after a delay
            savedStateHandle = savedState(lifecycleStatus = "open"),
        )
        // Before advanceUntilIdle, check intermediate state — should respect nav-arg until
        // the live emission completes
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
     * Room cache never changes, so the Room-backed observer alone would keep emitting the stale open
     * status forever. The periodic SERVER poll (fetchPackingRowStatus) closes that gap: on its next
     * tick it must flip the screen read-only without a back/reopen.
     */
    @Test
    fun `screen open editable flips read-only when the periodic server poll reports pending verification`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitPackingStatus(FeedStatus.PENDING) // Room cache: still open.

        val viewModel = FeedPackingCompleteViewModel(
            syncRepository = NoopFeedPackingSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            drafts = InMemoryCaptureDraftRepository(),
            feedRepository = feedRepository,
            feedCompletionStore = sg.mesha.goatos.core.data.FeedCompletionLocalStore(),
            savedStateHandle = savedState(lifecycleStatus = "open"),
        )
        // runCurrent() (NOT advanceUntilIdle()): the bounded server-poll loop is finite
        // (MAX_SERVER_STATUS_POLLS) but still spans ~24h of virtual time, and advanceUntilIdle()
        // would greedily run every one of those ticks right here, exhausting the poll budget before
        // this test gets to control it. runCurrent() only drains work that is already ready.
        runCurrent()
        assertFalse("screen starts editable", viewModel.state.value.alreadySubmitted)

        // The Room-backed source never changes; only the SERVER poll learns about the teammate's
        // submit.
        feedRepository.queueServerPackingStatus(FeedStatus.AWAITING)
        advanceTimeBy(30_001L)

        assertTrue(
            "a server-poll tick reporting pending_verification must flip the screen read-only",
            viewModel.state.value.alreadySubmitted,
        )
        assertFalse(viewModel.state.value.captureEnabled)
    }

    /**
     * FAIL-SOFT: a poll failure (offline/timeout/5xx) must leave the screen exactly as it was —
     * never flips editable -> locked on a guess.
     */
    @Test
    fun `a failed server poll tick leaves the editable state unchanged`() = runTest(dispatcher) {
        val feedRepository = FakeFeedRepository()
        feedRepository.emitPackingStatus(FeedStatus.PENDING)

        val viewModel = FeedPackingCompleteViewModel(
            syncRepository = NoopFeedPackingSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            drafts = InMemoryCaptureDraftRepository(),
            feedRepository = feedRepository,
            feedCompletionStore = sg.mesha.goatos.core.data.FeedCompletionLocalStore(),
            savedStateHandle = savedState(lifecycleStatus = "open"),
        )
        runCurrent() // see the sibling test above for why this is runCurrent(), not advanceUntilIdle().
        assertFalse(viewModel.state.value.alreadySubmitted)

        feedRepository.queueServerPackingFailure()
        advanceTimeBy(30_001L)

        assertFalse(
            "a failed poll tick must never flip the screen to read-only",
            viewModel.state.value.alreadySubmitted,
        )
    }
}

/** Everything no-ops; these tests never reach a write. */
private class NoopFeedPackingSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
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
    ): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = error("unused")
    override suspend fun enqueueFeedTransportSubmit(groupKey: String, idempotencyKey: String, taskId: String, proofOutboxItemId: String): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDistributionComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, distributionProofOutboxItemId: String?, feedWeightProofOutboxItemId: String?, waterProofOutboxItemId: String?, feedWeightProofRef: String?, distributionProofRef: String?, waterProofRef: String?): AppResult<String> = error("unused")
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
