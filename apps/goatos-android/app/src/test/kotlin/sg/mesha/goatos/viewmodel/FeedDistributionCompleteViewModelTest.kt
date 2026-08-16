package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
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
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.feature.feed.FeedDistributionEvent
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.core.network.dto.FeedDistributionCapturedSlotDto
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
            feedRepository = FakeSplitFeedRepository(emptyList()),
            feedCompletionStore = FeedCompletionLocalStore(),
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
        val survivingRows = proofCaptureRepository.allRows()
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
            feedRepository = FakeSplitFeedRepository(emptyList()),
            feedCompletionStore = FeedCompletionLocalStore(),
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
     * THREE operators, one pen-session: the phone that shot nothing must still be able to submit.
     *
     * A pen-session's three proofs may be split across three phones (maintainer decision
     * 2026-08-14). Each phone holds one of three local outbox ids, so before the shared read every
     * phone failed submit at `FEED_DISTRIBUTION_SUBMIT_BLOCKED` and the pen could never close.
     *
     * This drives the hardest version: THIS phone captured NOTHING. It must adopt all three
     * teammates' server proof ids, enable submit, and send those ids -- not local outbox references
     * it does not have.
     */
    @Test
    fun `a pen-session split across three operators is submittable from a phone that shot nothing`() = runTest(dispatcher) {
        val syncRepository = RecordingFeedDistributionSyncRepository()
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_feed_weight_photo",
                    proofRef = "server-proof-weight",
                    capturedAt = "2026-08-14T03:44:56Z",
                ),
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_video",
                    proofRef = "server-proof-feed-video",
                    capturedAt = "2026-08-14T03:45:26Z",
                ),
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_water_video",
                    proofRef = "server-proof-water-video",
                    capturedAt = "2026-08-14T03:45:47Z",
                ),
            ),
        )
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = syncRepository,
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "2",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "experiment",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-14",
                    FeedDistributionCompleteViewModel.ARG_PARTITION_LABEL to "Part 8",
                ),
            ),
        )
        runCurrent()

        // The shared read must be asked for THIS PEN, never the shed as a whole -- otherwise a
        // sibling pen's work would be claimed as this one's.
        assertEquals(1, teammates.queries.size)
        assertEquals("Part 8", teammates.queries.single().partitionLabel)
        assertEquals(2, teammates.queries.single().sessionNo)

        // All three slots read as done even though this phone captured nothing.
        assertEquals(true, viewModel.state.value.feedWeightPhotoCaptured)
        assertEquals(true, viewModel.state.value.videoCaptured)
        assertEquals(true, viewModel.state.value.waterVideoCaptured)
        assertEquals(true, viewModel.state.value.submitEnabled)

        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        runCurrent()

        assertEquals(1, syncRepository.completionEnqueueCount)
        // The SERVER proof ids travel, because there is no local outbox row to resolve.
        assertEquals("server-proof-weight", syncRepository.lastFeedWeightProofRef)
        assertEquals("server-proof-feed-video", syncRepository.lastDistributionProofRef)
        assertEquals("server-proof-water-video", syncRepository.lastWaterProofRef)
        assertEquals(null, syncRepository.lastFeedWeightProofOutboxItemId)
    }

    /**
     * A network blip during screen entry must NOT read as "no teammate proofs". The failed shape
     * observed on-device: operator A uploads a proof, operator B opens (or re-enters) the screen
     * while WiFi blips, the captures read fails, and B's screen stays stale claiming the slot is
     * free. The read must distinguish failure (null) from empty and retry until it gets an answer.
     */
    @Test
    fun `a failed teammate-captures read retries instead of treating the blip as no proofs`() = runTest(dispatcher) {
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_feed_weight_photo",
                    proofRef = "server-proof-weight",
                    capturedAt = "2026-08-15T02:04:00Z",
                ),
            ),
        ).apply { failuresBeforeSuccess = 2 }
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "1",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "experiment",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-15",
                ),
            ),
        )
        // Drain only the retry ladder (2s + 5s + 10s = 17s), never advanceUntilIdle -- the periodic
        // 30s server-poll loop is bounded but still spans ~24h of virtual time.
        runCurrent()
        advanceTimeBy(17_000L)
        runCurrent()

        // Two failures then a success: three reads total, and the slot still hydrates.
        assertEquals(3, teammates.queries.size)
        assertEquals(true, viewModel.state.value.feedWeightPhotoCaptured)
    }

    /**
     * Manual Sync must re-fetch teammate/server proof slots, not only drain the local outbox.
     * The stale shape: operator opens the screen at 1/3 slots, teammates upload the other two
     * while it stays open, operator taps Sync — before the fix the screen kept showing 1/3
     * until back/reopen, because refreshTeammateCaptures() ran on init only.
     */
    @Test
    fun `tapping Sync re-fetches teammate captures and unlocks submit with server refs`() = runTest(dispatcher) {
        val syncRepository = RecordingFeedDistributionSyncRepository()
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_feed_weight_photo",
                    proofRef = "server-proof-weight",
                    capturedAt = "2026-08-14T03:44:56Z",
                ),
            ),
        )
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = syncRepository,
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "2",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "experiment",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-14",
                    FeedDistributionCompleteViewModel.ARG_PARTITION_LABEL to "Part 8",
                ),
            ),
        )
        advanceUntilIdle()

        // Opened at 1/3: only the weight photo exists server-side.
        assertEquals(true, viewModel.state.value.feedWeightPhotoCaptured)
        assertEquals(false, viewModel.state.value.videoCaptured)
        assertEquals(false, viewModel.state.value.submitEnabled)

        // Teammates upload the remaining two slots while the screen stays open.
        teammates.slots = teammates.slots + listOf(
            FeedDistributionCapturedSlotDto(
                fieldKey = "feed_distribution_video",
                proofRef = "server-proof-feed-video",
                capturedAt = "2026-08-14T03:45:26Z",
            ),
            FeedDistributionCapturedSlotDto(
                fieldKey = "feed_distribution_water_video",
                proofRef = "server-proof-water-video",
                capturedAt = "2026-08-14T03:45:47Z",
            ),
        )

        viewModel.onEvent(FeedDistributionEvent.SyncNow)
        advanceUntilIdle()

        assertEquals(true, viewModel.state.value.videoCaptured)
        assertEquals(true, viewModel.state.value.waterVideoCaptured)
        assertEquals(true, viewModel.state.value.submitEnabled)

        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        advanceUntilIdle()

        assertEquals(1, syncRepository.completionEnqueueCount)
        assertEquals("server-proof-feed-video", syncRepository.lastDistributionProofRef)
        assertEquals("server-proof-water-video", syncRepository.lastWaterProofRef)
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
    /**
     * When a teammate re-captures a slot (uploads a different proof), the client must detect
     * the change and update the preview. The re-capture detection: server returns a DIFFERENT
     * proofRef for the same field_key. The client must OVERWRITE the previous ref and fetch
     * the new preview URL. Local captures always resist overwrite.
     */
    @Test
    fun `teammate re-capture overwrites adopted proof ref and updates preview URL`() = runTest(dispatcher) {
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_video",
                    proofRef = "server-proof-feed-video-v1",
                    capturedAt = "2026-08-14T03:45:26Z",
                    mimeType = "video/mp4",
                ),
            ),
        )
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "1",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "normal",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-14",
                    FeedDistributionCompleteViewModel.ARG_PARTITION_LABEL to "Part 1",
                ),
            ),
        )
        advanceUntilIdle()

        // Initial read: v1 proof is adopted, preview URL is fetched
        assertEquals(true, viewModel.state.value.videoCaptured)
        assertEquals("https://stg.example.com/proofs/server-proof-feed-video-v1/download?token=xyz", viewModel.state.value.videoRemoteUrl)

        // Teammate re-captures: server now returns v2
        teammates.slots = listOf(
            FeedDistributionCapturedSlotDto(
                fieldKey = "feed_distribution_video",
                proofRef = "server-proof-feed-video-v2",
                capturedAt = "2026-08-14T04:00:00Z",
                mimeType = "video/mp4",
            ),
        )

        // Manual sync re-fetches teammate captures
        viewModel.onEvent(FeedDistributionEvent.SyncNow)
        advanceUntilIdle()

        // State must reflect the NEW ref and fetch its preview URL
        assertEquals("https://stg.example.com/proofs/server-proof-feed-video-v2/download?token=xyz", viewModel.state.value.videoRemoteUrl)
    }

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
            feedRepository = FakeSplitFeedRepository(emptyList()),
            feedCompletionStore = FeedCompletionLocalStore(),
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
                    // A real pen, e.g. Godel 1 - Part 3. This is the whole point of the case.
                    FeedDistributionCompleteViewModel.ARG_PARTITION_LABEL to "Part 3",
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

    // --- New observability events (feed-distribution proof flow) ---

    @Test
    fun `split-operator submit fires submit_sources with all three server_ref`() = runTest(dispatcher) {
        val syncRepository = RecordingFeedDistributionSyncRepository()
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_feed_weight_photo",
                    proofRef = "server-proof-weight",
                    capturedAt = "2026-08-14T03:44:56Z",
                ),
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_video",
                    proofRef = "server-proof-feed-video",
                    capturedAt = "2026-08-14T03:45:26Z",
                ),
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_water_video",
                    proofRef = "server-proof-water-video",
                    capturedAt = "2026-08-14T03:45:47Z",
                ),
            ),
        )
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = syncRepository,
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "2",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "experiment",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-14",
                    FeedDistributionCompleteViewModel.ARG_PARTITION_LABEL to "Part 8",
                ),
            ),
        )
        advanceUntilIdle()
        assertEquals(true, viewModel.state.value.submitEnabled)

        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        advanceUntilIdle()

        val submitSources = analytics.events.last { it.name == "feed_distribution_submit_sources" }
        assertEquals("server_ref", submitSources.props["feed_weight_source"])
        assertEquals("server_ref", submitSources.props["feed_video_source"])
        assertEquals("server_ref", submitSources.props["water_video_source"])
        assertEquals("submitted", submitSources.props["result"])
    }

    @Test
    fun `a failed teammate-captures read emits failed then retry_exhausted without clearing status`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val teammates = FakeSplitFeedRepository(emptyList()).apply { failuresBeforeSuccess = 99 }
        FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "1",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "experiment",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-15",
                ),
            ),
        )
        // Same bounded window as the sibling retry test: drain only the retry ladder, never
        // advanceUntilIdle -- that would also drain the 24h-spanning server-poll loop, whose ticks
        // emit later success/failure reads that would corrupt "last event" assertions here.
        runCurrent()
        advanceTimeBy(17_000L)
        runCurrent()

        val reads = analytics.events.filter { it.name == "feed_distribution_teammate_captures_read" }
        assertEquals("retry_exhausted", reads.last().props["result"])
        assertEquals(true, reads.none { it.props["result"] == "success_empty" || it.props["result"] == "success_slots" })
    }

    @Test
    fun `an empty teammate-captures read fires success_empty and no proof_adopted event`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val teammates = FakeSplitFeedRepository(emptyList())
        FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = analytics,
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
        advanceUntilIdle()

        val reads = analytics.events.filter { it.name == "feed_distribution_teammate_captures_read" }
        assertEquals(true, reads.any { it.props["result"] == "success_empty" })
        assertEquals(true, analytics.events.none { it.name == "feed_distribution_teammate_proof_adopted" })
    }

    @Test
    fun `a locally-captured slot resists a teammate ref and does not overwrite local state`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val photoSource = FakePhotoCaptureSource(
            mutableListOf(CapturedPhoto(localUri = "/proof/feed-weight.jpg", capturedAtMs = 3L)),
        )
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_feed_weight_photo",
                    proofRef = "server-proof-weight",
                    capturedAt = "2026-08-14T03:44:56Z",
                ),
            ),
        )
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = photoSource,
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = analytics,
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
        // This phone captures the weight photo FIRST.
        viewModel.onEvent(FeedDistributionEvent.TakeFeedWeightPhoto)
        runCurrent()
        assertEquals(true, viewModel.state.value.feedWeightPhotoCaptured)
        assertEquals("/proof/feed-weight.jpg", viewModel.state.value.feedWeightPhotoPreviewPath)

        // Construction's own init-time refresh (before this phone captured anything) legitimately
        // adopts the teammate's ref once; clear that history so the assertion below is scoped to
        // the SyncNow tap under test, which is what must never re-adopt over the local capture.
        analytics.events.clear()

        // A teammate's server ref for the SAME slot must not overwrite the local capture.
        viewModel.onEvent(FeedDistributionEvent.SyncNow)
        runCurrent()

        assertEquals("/proof/feed-weight.jpg", viewModel.state.value.feedWeightPhotoPreviewPath)
        assertEquals(
            true,
            analytics.events.none {
                it.name == "feed_distribution_teammate_proof_adopted" && it.props["kind"] == "feed_weight_photo"
            },
        )
    }

    @Test
    fun `a teammate submitting while the screen is open flips it read-only via server_poll`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val feedRepository = object : FeedRepository by FakeSplitFeedRepository(emptyList()) {
            var statusToReturn: String? = "open"
            override suspend fun fetchDirectionSessionStatus(
                parkId: String,
                shedId: String,
                partitionLabel: String,
                workflow: String,
                sessionNo: Int,
                targetDate: String,
            ): String? = statusToReturn
        }
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = feedRepository,
            feedCompletionStore = FeedCompletionLocalStore(),
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "1",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "normal",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-08-12",
                    FeedDistributionCompleteViewModel.ARG_LIFECYCLE_STATUS to "open",
                ),
            ),
        )
        runCurrent()
        assertEquals(false, viewModel.state.value.alreadySubmitted)

        // Teammate submits elsewhere; the next periodic 30s server poll (not a manual Sync tap)
        // must observe it, without advancing through all MAX_SERVER_STATUS_POLLS iterations.
        feedRepository.statusToReturn = "pending_verification"
        advanceTimeBy(30_001L)
        runCurrent()

        assertEquals(true, viewModel.state.value.alreadySubmitted)
        val statusChange = analytics.events.last { it.name == "feed_distribution_live_status_changed" }
        assertEquals("server_poll", statusChange.props["source"])
        assertEquals("editable", statusChange.props["previous"])
        assertEquals("readonly", statusChange.props["next"])
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

    // Server proof ids for slots recorded on ANOTHER operator's phone.
    var lastFeedWeightProofRef: String? = null
    var lastDistributionProofRef: String? = null
    var lastWaterProofRef: String? = null

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
        distributionProofOutboxItemId: String?,
        feedWeightProofOutboxItemId: String?,
        waterProofOutboxItemId: String?,
        feedWeightProofRef: String?,
        distributionProofRef: String?,
        waterProofRef: String?,
    ): AppResult<String> {
        completionEnqueueCount += 1
        lastFeedWeightProofOutboxItemId = feedWeightProofOutboxItemId
        lastDistributionProofOutboxItemId = distributionProofOutboxItemId
        lastWaterProofOutboxItemId = waterProofOutboxItemId
        lastFeedWeightProofRef = feedWeightProofRef
        lastDistributionProofRef = distributionProofRef
        lastWaterProofRef = waterProofRef
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
