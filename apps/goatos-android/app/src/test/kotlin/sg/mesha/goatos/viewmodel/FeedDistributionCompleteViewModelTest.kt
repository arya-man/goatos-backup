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
import org.junit.Assert.assertTrue
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
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.network.dto.FeedSopCardDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.core.network.dto.WeighingSopOptionDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.feature.feed.FeedSlotCaptureKind


private const val WEIGHT_SLOT = "feed_distribution_feed_weight_photo"
private const val FEED_SLOT = "feed_distribution_video"
private const val WATER_SLOT = "feed_distribution_water_video"
private val sg.mesha.goatos.feature.feed.FeedDistributionUiState.weightSlot get() = slot(WEIGHT_SLOT) ?: error("weight slot not on the card")
private val sg.mesha.goatos.feature.feed.FeedDistributionUiState.feedSlot get() = slot(FEED_SLOT) ?: error("feed slot not on the card")
private val sg.mesha.goatos.feature.feed.FeedDistributionUiState.waterSlot get() = slot(WATER_SLOT) ?: error("water slot not on the card")


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

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WEIGHT_SLOT))
        advanceUntilIdle()
        assertEquals("the first capture must record one proof", 1, proofCaptureRepository.captureCalls.size)
        assertEquals(true, viewModel.state.value.weightSlot.captured)

        // Retake, and cancel it.
        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WEIGHT_SLOT))
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
    fun `missing proof upload outbox marks feed video failed and records failure event`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository().apply {
            omitNextOutboxItem = true
        }
        val analytics = FakeAnalyticsPort()
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(
                mutableListOf(CapturedVideo(localUri = "/proof/feed.mp4", startedAtMs = 1L, endedAtMs = 2L)),
            ),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = proofCaptureRepository,
            feedRepository = FakeSplitFeedRepository(emptyList()),
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

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(FEED_SLOT))
        advanceUntilIdle()

        assertEquals(FeedDistributionProofStatus.FAILED, viewModel.state.value.feedSlot.status)
        assertEquals(false, viewModel.state.value.submitEnabled)
        assertEquals(
            true,
            analytics.events.any {
                it.first == "feed_distribution_failure" &&
                    it.second["kind"] == "feed_video" &&
                    it.second["reason"] == "missing_upload_outbox"
            },
        )
    }

    @Test
    fun `missing proof upload outbox marks feed weight photo failed and records failure event`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository().apply {
            omitNextOutboxItem = true
        }
        val analytics = FakeAnalyticsPort()
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(
                mutableListOf(CapturedPhoto(localUri = "/proof/feed-weight.jpg", capturedAtMs = 1L)),
            ),
            proofCaptureRepository = proofCaptureRepository,
            feedRepository = FakeSplitFeedRepository(emptyList()),
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

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WEIGHT_SLOT))
        advanceUntilIdle()

        assertEquals(FeedDistributionProofStatus.FAILED, viewModel.state.value.weightSlot.status)
        assertEquals(
            true,
            analytics.events.any {
                it.first == "feed_distribution_failure" &&
                    it.second["kind"] == "feed_weight_photo" &&
                    it.second["reason"] == "missing_upload_outbox"
            },
        )
    }

    @Test
    fun `missing proof upload outbox marks water video failed and records failure event`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository().apply {
            omitNextOutboxItem = true
        }
        val analytics = FakeAnalyticsPort()
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(
                mutableListOf(CapturedVideo(localUri = "/proof/water.mp4", startedAtMs = 1L, endedAtMs = 2L)),
            ),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = proofCaptureRepository,
            feedRepository = FakeSplitFeedRepository(emptyList()),
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

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WATER_SLOT))
        advanceUntilIdle()

        assertEquals(FeedDistributionProofStatus.FAILED, viewModel.state.value.waterSlot.status)
        assertEquals(
            true,
            analytics.events.any {
                it.first == "feed_distribution_failure" &&
                    it.second["kind"] == "water_video" &&
                    it.second["reason"] == "missing_upload_outbox"
            },
        )
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

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WEIGHT_SLOT))
        advanceUntilIdle()
        assertEquals(0, syncRepository.completionEnqueueCount)

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(FEED_SLOT))
        advanceUntilIdle()
        assertEquals(0, syncRepository.completionEnqueueCount)

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WATER_SLOT))
        advanceUntilIdle()
        assertEquals(0, syncRepository.completionEnqueueCount)
        assertEquals(true, viewModel.state.value.submitEnabled)

        syncRepository.setItemStatus("proof-outbox-1", SyncItemStatus.SUCCEEDED)
        syncRepository.setItemStatus("proof-outbox-2", SyncItemStatus.SUCCEEDED)
        syncRepository.setItemStatus("proof-outbox-3", SyncItemStatus.SUCCEEDED)
        advanceUntilIdle()
        assertEquals(FeedDistributionProofStatus.SYNCED, viewModel.state.value.weightSlot.status)
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

    @Test
    fun `distribution proof uploads share completion outbox group so completion waits behind proofs`() = runTest(dispatcher) {
        val syncRepository = RecordingFeedDistributionSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = syncRepository,
            proofCaptureSource = FakeProofCaptureSource(
                mutableListOf(
                    CapturedVideo(localUri = "/proof/feed.mp4", startedAtMs = 1L, endedAtMs = 2L),
                    CapturedVideo(localUri = "/proof/water.mp4", startedAtMs = 3L, endedAtMs = 4L),
                ),
            ),
            photoCaptureSource = FakePhotoCaptureSource(
                mutableListOf(CapturedPhoto(localUri = "/proof/feed-weight.jpg", capturedAtMs = 3L)),
            ),
            proofCaptureRepository = proofCaptureRepository,
            feedRepository = FakeSplitFeedRepository(emptyList()),
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
                    FeedDistributionCompleteViewModel.ARG_PARTITION_LABEL to "Part 3",
                ),
            ),
        )

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WEIGHT_SLOT))
        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(FEED_SLOT))
        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WATER_SLOT))
        advanceUntilIdle()

        val sessionGroup = "feed-dist:2026-08-12:shed-1:part 3:1:normal"
        assertEquals(
            listOf(
                sessionGroup,
                sessionGroup,
                sessionGroup,
            ),
            proofCaptureRepository.captureCalls.map { it.uploadGroupKey },
        )
        assertEquals(
            "completion must share the proof upload lane so it cannot close before a required proof drains",
            1,
            proofCaptureRepository.captureCalls.map { it.uploadGroupKey }.distinct().size,
        )

        syncRepository.setItemStatus("proof-outbox-1", SyncItemStatus.SUCCEEDED)
        syncRepository.setItemStatus("proof-outbox-2", SyncItemStatus.SUCCEEDED)
        syncRepository.setItemStatus("proof-outbox-3", SyncItemStatus.SUCCEEDED)
        advanceUntilIdle()
        viewModel.onEvent(FeedDistributionEvent.MarkDone)
        advanceUntilIdle()

        assertEquals(sessionGroup, syncRepository.lastCompletionGroupKey)
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
        assertEquals(true, viewModel.state.value.weightSlot.captured)
        assertEquals(true, viewModel.state.value.feedSlot.captured)
        assertEquals(true, viewModel.state.value.waterSlot.captured)
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
        assertEquals(true, viewModel.state.value.weightSlot.captured)
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
        assertEquals(true, viewModel.state.value.weightSlot.captured)
        assertEquals(false, viewModel.state.value.feedSlot.captured)
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

        assertEquals(true, viewModel.state.value.feedSlot.captured)
        assertEquals(true, viewModel.state.value.waterSlot.captured)
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

        // Initial read: v1 proof is adopted, but the app keeps the backend proof route stable.
        assertEquals(true, viewModel.state.value.feedSlot.captured)
        assertTrue(viewModel.state.value.feedSlot.remoteUrl?.endsWith("/app/proofs/server-proof-feed-video-v1/download") == true)

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

        // State must reflect the NEW ref without caching a short-lived signed GCS URL.
        assertTrue(viewModel.state.value.feedSlot.remoteUrl?.endsWith("/app/proofs/server-proof-feed-video-v2/download") == true)
    }

    @Test
    fun `local video replacement after teammate adoption uses local preview identity`() = runTest(dispatcher) {
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_video",
                    proofRef = "server-proof-feed-video",
                    capturedAt = "2026-08-14T03:45:26Z",
                    mimeType = "video/mp4",
                ),
            ),
        )
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(
                mutableListOf(CapturedVideo(localUri = "/proof/local-feed-replacement.mp4", startedAtMs = 1L, endedAtMs = 2L)),
            ),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
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

        assertEquals("server-proof-feed-video", viewModel.state.value.feedSlot.previewIdentity)

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(FEED_SLOT))
        advanceUntilIdle()

        assertEquals("/proof/local-feed-replacement.mp4", viewModel.state.value.feedSlot.previewPath)
        assertEquals("proof-outbox-1", viewModel.state.value.feedSlot.previewIdentity)
    }

    @Test
    fun `local water video replacement after teammate adoption uses local preview identity`() = runTest(dispatcher) {
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_water_video",
                    proofRef = "server-proof-water-video",
                    capturedAt = "2026-08-14T03:45:26Z",
                    mimeType = "video/mp4",
                ),
            ),
        )
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(
                mutableListOf(CapturedVideo(localUri = "/proof/local-water-replacement.mp4", startedAtMs = 1L, endedAtMs = 2L)),
            ),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
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

        assertEquals("server-proof-water-video", viewModel.state.value.waterSlot.previewIdentity)

        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WATER_SLOT))
        advanceUntilIdle()

        assertEquals("/proof/local-water-replacement.mp4", viewModel.state.value.waterSlot.previewPath)
        assertEquals("proof-outbox-1", viewModel.state.value.waterSlot.previewIdentity)
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
        first.onEvent(FeedDistributionEvent.CaptureSlot(FEED_SLOT))
        advanceUntilIdle()
        assertEquals(true, first.state.value.feedSlot.captured)

        // Back navigation pops the entry and clears the view model; reopening builds a fresh one
        // against the same durable store.
        val reopened = viewModelForPen()
        advanceUntilIdle()

        assertEquals(true, reopened.state.value.feedSlot.captured)
        assertEquals("/proof/feed.mp4", reopened.state.value.feedSlot.previewPath)
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
        viewModel.onEvent(FeedDistributionEvent.CaptureSlot(WEIGHT_SLOT))
        runCurrent()
        assertEquals(true, viewModel.state.value.weightSlot.captured)
        assertEquals("/proof/feed-weight.jpg", viewModel.state.value.weightSlot.previewPath)

        // Construction's own init-time refresh (before this phone captured anything) legitimately
        // adopts the teammate's ref once; clear that history so the assertion below is scoped to
        // the SyncNow tap under test, which is what must never re-adopt over the local capture.
        analytics.events.clear()

        // A teammate's server ref for the SAME slot must not overwrite the local capture.
        viewModel.onEvent(FeedDistributionEvent.SyncNow)
        runCurrent()

        assertEquals("/proof/feed-weight.jpg", viewModel.state.value.weightSlot.previewPath)
        assertEquals(
            true,
            analytics.events.none {
                it.name == "feed_distribution_teammate_proof_adopted" && it.props["kind"] == "feed_weight_photo"
            },
        )
    }

    @Test
    fun `feed video playback failure keeps the same backend proof route`() = runTest(dispatcher) {
        val teammates = FakeSplitFeedRepository(
            listOf(
                FeedDistributionCapturedSlotDto(
                    fieldKey = "feed_distribution_video",
                    proofRef = "server-proof-feed-video",
                    capturedAt = "2026-08-14T03:45:26Z",
                ),
            ),
        )
        teammates.downloadUrls += "https://stg.example.com/proofs/server-proof-feed-video/download?token=old"
        teammates.downloadUrls += "https://stg.example.com/proofs/server-proof-feed-video/download?token=fresh"
        val viewModel = FeedDistributionCompleteViewModel(
            syncRepository = RecordingFeedDistributionSyncRepository(),
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = teammates,
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
                    FeedDistributionCompleteViewModel.ARG_PARTITION_LABEL to "Part 3",
                ),
            ),
        )
        advanceUntilIdle()
        assertTrue(viewModel.state.value.feedSlot.remoteUrl?.endsWith("/app/proofs/server-proof-feed-video/download") == true)

        viewModel.onEvent(FeedDistributionEvent.SlotPlaybackFailed(FEED_SLOT))
        advanceUntilIdle()

        assertEquals(emptyList<String>(), teammates.downloadProofIds)
        assertTrue(viewModel.state.value.feedSlot.remoteUrl?.endsWith("/app/proofs/server-proof-feed-video/download") == true)
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
    var lastCompletionGroupKey: String? = null
        private set

    // Server proof ids for slots recorded on ANOTHER operator's phone.
    var lastFeedWeightProofRef: String? = null
    var lastDistributionProofRef: String? = null
    var lastWaterProofRef: String? = null
    var lastSlotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload> = emptyMap()
    var lastAnswers: kotlinx.serialization.json.JsonObject = kotlinx.serialization.json.JsonObject(emptyMap())

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
        slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>,
        answers: kotlinx.serialization.json.JsonObject,
    ): AppResult<String> {
        completionEnqueueCount += 1
        lastCompletionGroupKey = groupKey
        lastFeedWeightProofOutboxItemId = feedWeightProofOutboxItemId
        lastDistributionProofOutboxItemId = distributionProofOutboxItemId
        lastWaterProofOutboxItemId = waterProofOutboxItemId
        lastFeedWeightProofRef = feedWeightProofRef
        lastDistributionProofRef = distributionProofRef
        lastWaterProofRef = waterProofRef
        lastSlotProofs = slotProofs
        lastAnswers = answers
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
        measurement: VerificationVerdictMeasurementDto?,
    ): AppResult<String> = error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    val deletedItemIds = mutableListOf<String>()
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> {
        deletedItemIds += itemId
        items[itemId]?.value = null
        return AppResult.Ok(Unit)
    }
    /** Puts the completion item into the terminal state a 422 leaves it in (conflict dead letter). */
    fun rejectCompletion(itemId: String) {
        items.getOrPut(itemId) { MutableStateFlow(null) }.value = SyncQueueItem(
            id = itemId, opType = "FEED_DISTRIBUTION_COMPLETE", idempotencyKey = "completion-key-$itemId",
            groupKey = "feed-dist:shed-1:1:normal", status = SyncItemStatus.FAILED, attemptCount = 1, maxAttempts = 3,
            conflict = true, createdAt = 1L, updatedAt = 2L, lastError = "Enter a value between 1 and 200",
        )
    }
    override suspend fun triggerDrain() = Unit
}

/**
 * FEED SOP (maintainer decision 2026-09-16): the distribution screen renders the CARD the sheet is
 * pinned to, not a compiled-in trio. A slot the maintainer adds appears, one removed disappears,
 * an optional one never gates, a question is asked and its answer travels with the submit -- all
 * without a new build. The collaboration contract is unchanged underneath: a slot shot on another
 * phone is adopted by key, including a slot that did not exist when this APK was built.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class FeedDistributionCardDrivenTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before fun setUp() { Dispatchers.setMain(dispatcher) }
    @After fun tearDown() { Dispatchers.resetMain() }

    private val weight = "feed_distribution_feed_weight_photo"
    private val feed = "feed_distribution_video"
    private val water = "feed_distribution_water_video"
    private val trough = "trough_after_feeding"

    private fun seededSlots() = listOf(
        WeighingRemovalProofSlotDto(key = weight, title = "Feed weight photo", kind = "photo", required = true),
        WeighingRemovalProofSlotDto(key = feed, title = "Feed distribution video", kind = "video", required = true),
        WeighingRemovalProofSlotDto(key = water, title = "Water distribution video", kind = "video", required = true),
    )

    private fun viewModel(repo: FakeSplitFeedRepository, sync: RecordingFeedDistributionSyncRepository = RecordingFeedDistributionSyncRepository(), analytics: RecordingAnalytics = RecordingAnalytics()) =
        FeedDistributionCompleteViewModel(
            syncRepository = sync,
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            photoCaptureSource = FakePhotoCaptureSource(mutableListOf()),
            proofCaptureRepository = FakeProofCaptureRepository(),
            feedRepository = repo,
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    FeedDistributionCompleteViewModel.ARG_PARK_ID to "park-1",
                    FeedDistributionCompleteViewModel.ARG_SHED_ID to "shed-1",
                    FeedDistributionCompleteViewModel.ARG_SESSION_NO to "1",
                    FeedDistributionCompleteViewModel.ARG_WORKFLOW to "normal",
                    FeedDistributionCompleteViewModel.ARG_TARGET_DATE to "2026-09-16",
                    FeedDistributionCompleteViewModel.ARG_PARTITION_LABEL to "Part 1",
                ),
            ),
        )

    @Test
    fun `the seeded trio shows before any card arrives and the live card reshapes it`() = runTest(dispatcher) {
        val repo = FakeSplitFeedRepository(emptyList())
        repo.failuresBeforeSuccess = 99 // offline: the live read never answers
        val vm = viewModel(repo)
        runCurrent()
        assertEquals(listOf(weight, feed, water), vm.state.value.slots.map { it.slotKey })
        assertEquals(FeedSlotCaptureKind.PHOTO, vm.state.value.slots[0].captureKind)
        assertTrue(vm.state.value.slots.all { it.required })
        assertTrue(vm.state.value.instruction.isNotBlank())
    }

    @Test
    fun `a slot the maintainer adds appears and an optional one never gates the submit`() = runTest(dispatcher) {
        val repo = FakeSplitFeedRepository(
            slots = listOf(
                FeedDistributionCapturedSlotDto(fieldKey = weight, proofRef = "srv-w", capturedAt = "2026-09-16T03:00:00Z", mimeType = "image/jpeg"),
                FeedDistributionCapturedSlotDto(fieldKey = feed, proofRef = "srv-f", capturedAt = "2026-09-16T03:01:00Z"),
                FeedDistributionCapturedSlotDto(fieldKey = water, proofRef = "srv-wa", capturedAt = "2026-09-16T03:02:00Z"),
            ),
            card = FeedSopCardDto(
                version = 2,
                stage = "distribution",
                instruction = "Film everything.",
                proofs = seededSlots() + WeighingRemovalProofSlotDto(key = trough, title = "Trough after feeding", kind = "either", required = false),
            ),
        )
        val sync = RecordingFeedDistributionSyncRepository()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repo, sync, analytics)
        runCurrent()

        assertEquals(listOf(weight, feed, water, trough), vm.state.value.slots.map { it.slotKey })
        assertEquals("Film everything.", vm.state.value.instruction)
        val added = vm.state.value.slot(trough)!!
        assertFalse(added.required)
        assertEquals(FeedSlotCaptureKind.EITHER, added.captureKind)
        assertFalse(added.captured)
        // The three teammate proofs were adopted by key on the card-shaped list.
        assertTrue(vm.state.value.slot(weight)!!.captured)
        assertEquals(FeedSlotCaptureKind.PHOTO, vm.state.value.slot(weight)!!.capturedKind)
        assertTrue(vm.state.value.submitEnabled)

        vm.onEvent(FeedDistributionEvent.MarkDone)
        runCurrent()
        assertEquals(1, sync.completionEnqueueCount)
        // Only captured slots are named; the empty optional one is not.
        assertEquals(setOf(weight, feed, water), sync.lastSlotProofs.keys)
        assertEquals("srv-f", sync.lastSlotProofs[feed]!!.proofRef)
        // The legacy mirror still carries the seeded trio for the dispatcher's fixed fields.
        assertEquals("srv-w", sync.lastFeedWeightProofRef)
        assertTrue(analytics.events.any { it.name == "feed_distribution_card_applied" && it.props["sop_version"] == "2" })
    }

    @Test
    fun `a compulsory slot the maintainer adds gates the submit until a teammate fills it`() = runTest(dispatcher) {
        val repo = FakeSplitFeedRepository(
            slots = listOf(
                FeedDistributionCapturedSlotDto(fieldKey = weight, proofRef = "srv-w", capturedAt = "2026-09-16T03:00:00Z"),
                FeedDistributionCapturedSlotDto(fieldKey = feed, proofRef = "srv-f", capturedAt = "2026-09-16T03:01:00Z"),
                FeedDistributionCapturedSlotDto(fieldKey = water, proofRef = "srv-wa", capturedAt = "2026-09-16T03:02:00Z"),
            ),
            card = FeedSopCardDto(
                version = 3,
                stage = "distribution",
                proofs = seededSlots() + WeighingRemovalProofSlotDto(key = trough, title = "Trough after feeding", kind = "photo", required = true),
            ),
        )
        val sync = RecordingFeedDistributionSyncRepository()
        val vm = viewModel(repo, sync)
        runCurrent()
        assertFalse("the new compulsory slot is empty, so the pen cannot be submitted", vm.state.value.submitEnabled)
        vm.onEvent(FeedDistributionEvent.MarkDone)
        runCurrent()
        assertEquals(0, sync.completionEnqueueCount)

        // A teammate shoots the new slot on a phone built after the card changed; the next read adopts it.
        repo.slots = repo.slots + FeedDistributionCapturedSlotDto(fieldKey = trough, proofRef = "srv-t", capturedAt = "2026-09-16T03:05:00Z", mimeType = "image/jpeg")
        vm.onEvent(FeedDistributionEvent.SyncNow)
        runCurrent()
        assertTrue(vm.state.value.slot(trough)!!.captured)
        assertTrue(vm.state.value.submitEnabled)
        vm.onEvent(FeedDistributionEvent.MarkDone)
        runCurrent()
        assertEquals(1, sync.completionEnqueueCount)
        assertEquals("srv-t", sync.lastSlotProofs[trough]!!.proofRef)
    }

    @Test
    fun `a slot the maintainer removes leaves the screen and the submit`() = runTest(dispatcher) {
        val repo = FakeSplitFeedRepository(
            slots = listOf(
                FeedDistributionCapturedSlotDto(fieldKey = weight, proofRef = "srv-w", capturedAt = "2026-09-16T03:00:00Z"),
                FeedDistributionCapturedSlotDto(fieldKey = feed, proofRef = "srv-f", capturedAt = "2026-09-16T03:01:00Z"),
            ),
            card = FeedSopCardDto(version = 4, stage = "distribution", proofs = seededSlots().filter { it.key != water }),
        )
        val sync = RecordingFeedDistributionSyncRepository()
        val vm = viewModel(repo, sync)
        runCurrent()
        assertEquals(listOf(weight, feed), vm.state.value.slots.map { it.slotKey })
        assertNull(vm.state.value.slot(water))
        assertTrue(vm.state.value.submitEnabled)
        vm.onEvent(FeedDistributionEvent.MarkDone)
        runCurrent()
        assertEquals(setOf(weight, feed), sync.lastSlotProofs.keys)
        assertNull(sync.lastWaterProofRef)
    }

    @Test
    fun `the Room-cached card shapes the screen offline and the live card wins over it`() = runTest(dispatcher) {
        val repo = FakeSplitFeedRepository(emptyList())
        repo.failuresBeforeSuccess = 99
        repo.cachedCard.value = FeedSopCardDto(version = 5, stage = "distribution", proofs = seededSlots() + WeighingRemovalProofSlotDto(key = trough, title = "Trough", kind = "photo", required = false))
        val vm = viewModel(repo)
        runCurrent()
        assertEquals(4, vm.state.value.slots.size)

        // Back online: the open-time read's retry ladder now succeeds, carrying a newer card with
        // the extra slot gone again.
        repo.failuresBeforeSuccess = 0
        repo.card = FeedSopCardDto(version = 6, stage = "distribution", proofs = seededSlots())
        advanceTimeBy(3_000)
        runCurrent()
        assertEquals(3, vm.state.value.slots.size)
        // A stale cache emission after the live card must not put the slot back.
        repo.cachedCard.value = FeedSopCardDto(version = 5, stage = "distribution", proofs = seededSlots() + WeighingRemovalProofSlotDto(key = "stale", title = "Stale", kind = "photo", required = false))
        runCurrent()
        assertEquals(3, vm.state.value.slots.size)
    }

    @Test
    fun `card questions are asked, required ones gate the submit, and answers travel typed`() = runTest(dispatcher) {
        val repo = FakeSplitFeedRepository(
            slots = listOf(
                FeedDistributionCapturedSlotDto(fieldKey = weight, proofRef = "srv-w", capturedAt = "2026-09-16T03:00:00Z"),
                FeedDistributionCapturedSlotDto(fieldKey = feed, proofRef = "srv-f", capturedAt = "2026-09-16T03:01:00Z"),
                FeedDistributionCapturedSlotDto(fieldKey = water, proofRef = "srv-wa", capturedAt = "2026-09-16T03:02:00Z"),
            ),
            card = FeedSopCardDto(
                version = 7,
                stage = "distribution",
                proofs = seededSlots(),
                questions = listOf(
                    WeighingSopQuestionDto(id = "trough_clean", kind = "choice", title = "Was the trough clean?", required = true, options = listOf(WeighingSopOptionDto("yes", "Yes"), WeighingSopOptionDto("no", "No"))),
                    WeighingSopQuestionDto(id = "leftover_kg", kind = "number", title = "Leftover", required = false, unit = "kg"),
                ),
            ),
        )
        val sync = RecordingFeedDistributionSyncRepository()
        val vm = viewModel(repo, sync)
        runCurrent()
        assertEquals(2, vm.state.value.questions.size)
        assertFalse("the required question is unanswered", vm.state.value.submitEnabled)
        vm.onEvent(FeedDistributionEvent.Answer("trough_clean", "yes"))
        vm.onEvent(FeedDistributionEvent.Answer("leftover_kg", "1.5"))
        runCurrent()
        assertTrue(vm.state.value.submitEnabled)
        vm.onEvent(FeedDistributionEvent.MarkDone)
        runCurrent()
        assertEquals(1, sync.completionEnqueueCount)
        assertEquals("yes", sync.lastAnswers["trough_clean"]!!.jsonPrimitive.content)
        assertEquals(JsonPrimitive(1.5), sync.lastAnswers["leftover_kg"])
    }

    @Test
    fun `slots stay independent on a card-shaped list`() = runTest(dispatcher) {
        val repo = FakeSplitFeedRepository(
            slots = emptyList(),
            card = FeedSopCardDto(version = 8, stage = "distribution", proofs = seededSlots() + WeighingRemovalProofSlotDto(key = trough, title = "Trough", kind = "video", required = true)),
        )
        val vm = viewModel(repo)
        runCurrent()
        // Every slot is enabled at once; nothing has to come first, including the new one.
        assertTrue(vm.state.value.slots.all { it.captureEnabled && it.status == FeedDistributionProofStatus.EMPTY })
    }

    @Test
    fun `a corrected resubmit retires its rejected predecessor instead of queueing behind it`() = runTest(dispatcher) {
        val repo = FakeSplitFeedRepository(
            slots = listOf(
                FeedDistributionCapturedSlotDto(fieldKey = weight, proofRef = "srv-w", capturedAt = "2026-09-16T03:00:00Z"),
                FeedDistributionCapturedSlotDto(fieldKey = feed, proofRef = "srv-f", capturedAt = "2026-09-16T03:01:00Z"),
                FeedDistributionCapturedSlotDto(fieldKey = water, proofRef = "srv-wa", capturedAt = "2026-09-16T03:02:00Z"),
            ),
            card = FeedSopCardDto(
                version = 9, stage = "distribution", proofs = seededSlots(),
                questions = listOf(WeighingSopQuestionDto(id = "bags", kind = "number", title = "How many bags?", required = true, min = 1.0, max = 200.0)),
            ),
        )
        val sync = RecordingFeedDistributionSyncRepository()
        val vm = viewModel(repo, sync)
        runCurrent()
        vm.onEvent(FeedDistributionEvent.Answer("bags", "500"))
        vm.onEvent(FeedDistributionEvent.MarkDone)
        runCurrent()
        assertEquals(1, sync.completionEnqueueCount)
        // The server refuses it (422 -> terminal conflict); the screen offers Submit again.
        sync.rejectCompletion("completion-item-1")
        runCurrent()
        assertTrue(vm.state.value.canComplete)
        // The operator corrects the answer -> a different proof-set+answers key.
        vm.onEvent(FeedDistributionEvent.Answer("bags", "12"))
        vm.onEvent(FeedDistributionEvent.MarkDone)
        runCurrent()
        assertEquals(2, sync.completionEnqueueCount)
        assertEquals(
            "the rejected submit must be retired so the corrected one is not stuck behind a dead letter in the same lane",
            listOf("completion-item-1"), sync.deletedItemIds,
        )
        assertEquals(JsonPrimitive(12.0), sync.lastAnswers["bags"])
    }
}
