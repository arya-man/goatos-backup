package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
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
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenVisits
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.PEN_VISIT_VIDEO_FIELD_KEY
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.penVisitGrainKey
import sg.mesha.goatos.feature.penvisits.PenVisitDetailEvent
import sg.mesha.goatos.feature.penvisits.PenVisitTone
import sg.mesha.goatos.feature.penvisits.PenVisitVideoState

/**
 * ONE pen visit's detail state holder (maintainer decision 2026-09-07).
 *
 * The rules these hold: the SERVER's `can_submit` alone opens the camera; a real recording
 * reaches the durable proof slot AND queues its submit against that proof row, on the visit's
 * own FIFO lane, carrying the row version on screen; the slot state is derived from Room truth
 * (server task, proof row, outbox row) with the server's `Done` outranking everything; every
 * visible word is the backend's.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class PenVisitDetailViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `renders backend copy verbatim and offers the camera only on can_submit`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(penVisit(stateChip = "Due today", stateTone = "info"))
        val vm = viewModel(repository)
        advanceUntilIdle()

        val state = vm.state.value
        assertFalse(state.loading)
        assertEquals("Visit Castro 2 · Coimbatore", state.title)
        assertEquals("Castro 2", state.penLabel)
        assertEquals("Vaccination yesterday", state.reasonLine)
        assertEquals("Due today", state.stateChip)
        assertEquals(PenVisitTone.INFO, state.tone)
        assertEquals("Walk the pen and record one video of the animals.", state.instruction)
        assertTrue(state.canSubmit)
        assertEquals(PenVisitVideoState.EMPTY, state.videoState)
        // Opening the screen re-reads the server's truth once.
        assertEquals(1, repository.refreshDetailCalls)
    }

    @Test
    fun `a visit the caller may not submit opens no camera and queues nothing`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(penVisit(canSubmit = false))
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(video())
        val sync = RecordingPenVisitSyncRepository()
        val vm = viewModel(repository, captureSource = captureSource, sync = sync)
        advanceUntilIdle()

        vm.onEvent(PenVisitDetailEvent.RecordVideo)
        advanceUntilIdle()

        // The camera never opened and no write was queued: only the server opens a visit, and
        // asking it again (the refresh) is the honest response to a stale screen.
        assertEquals(0, captureSource.captureCount)
        assertTrue(sync.submits.isEmpty())
        assertEquals(2, repository.refreshDetailCalls)
    }

    @Test
    fun `a real recording lands in the durable slot and queues the submit against its proof row`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(penVisit(rowVersion = 7))
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(video())
        val sync = RecordingPenVisitSyncRepository()
        val proofs = FakeProofCaptureRepository()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, captureSource, sync, proofs, analytics)
        advanceUntilIdle()

        vm.onEvent(PenVisitDetailEvent.RecordVideo)
        advanceUntilIdle()

        assertEquals(1, captureSource.captureCount)
        // The recorder chrome carries the backend's own words.
        assertEquals("Visit Castro 2 · Coimbatore", captureSource.captureContexts.single()?.title)
        assertEquals("Castro 2", captureSource.captureContexts.single()?.primaryTag)

        val capture = proofs.captureCalls.single()
        // One capture identity per visit, so a re-record replaces rather than accumulates …
        assertEquals("visit-video", capture.fieldKey)
        assertEquals(PEN_VISIT_TEST_TASK_ID, capture.subjectId)
        // … on ONE FIFO lane per visit, so the upload always drains before the submit.
        assertEquals("pen-visit:task:$PEN_VISIT_TEST_TASK_ID", capture.uploadGroupKey)

        val queued = sync.submits.single()
        assertEquals(PEN_VISIT_TEST_TASK_ID, queued.taskId)
        // The row version the screen rendered rides the submit (the server fences on it).
        assertEquals(7, queued.rowVersion)
        assertTrue(queued.proofOutboxItemId.isNotBlank())

        // The whole funnel, in order, and no failure.
        val names = analytics.events.map { it.name }
        listOf(
            AnalyticsEventsPenVisits.CAPTURE_STARTED,
            AnalyticsEventsPenVisits.CAPTURE_RESULT,
            AnalyticsEventsPenVisits.ROOM_WRITTEN,
            AnalyticsEventsPenVisits.UPLOAD_ENQUEUED,
            AnalyticsEventsPenVisits.SUBMITTED,
        ).forEach { assertTrue("$it fired", it in names) }
        assertEquals("recorded", analytics.events.single { it.name == AnalyticsEventsPenVisits.CAPTURE_RESULT }.props[AnalyticsEvents.Params.RESULT])
        assertFalse(AnalyticsEventsPenVisits.FAILURE in names)
        // The slot reads as on its way, with the clip shown back.
        assertEquals(PenVisitVideoState.WORKING, vm.state.value.videoState)
        assertTrue(vm.state.value.previewPath.isNotBlank())
    }

    @Test
    fun `a cancelled recording writes nothing and reports the result`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(penVisit())
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(null)
        val sync = RecordingPenVisitSyncRepository()
        val proofs = FakeProofCaptureRepository()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, captureSource, sync, proofs, analytics)
        advanceUntilIdle()

        vm.onEvent(PenVisitDetailEvent.RecordVideo)
        advanceUntilIdle()

        assertEquals(1, captureSource.captureCount)
        assertTrue(proofs.captureCalls.isEmpty())
        assertTrue(sync.submits.isEmpty())
        assertEquals("cancelled", analytics.events.single { it.name == AnalyticsEventsPenVisits.CAPTURE_RESULT }.props[AnalyticsEvents.Params.RESULT])
        assertEquals(PenVisitVideoState.EMPTY, vm.state.value.videoState)
        assertFalse(vm.state.value.capturing)
    }

    @Test
    fun `a submit enqueue failure keeps the clip and tells the park head`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(penVisit())
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(video())
        val sync = RecordingPenVisitSyncRepository().apply { failNextSubmit = true }
        val proofs = FakeProofCaptureRepository()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, captureSource, sync, proofs, analytics)
        advanceUntilIdle()

        vm.onEvent(PenVisitDetailEvent.RecordVideo)
        advanceUntilIdle()

        assertEquals(1, proofs.captureCalls.size)
        val failure = analytics.events.single { it.name == AnalyticsEventsPenVisits.FAILURE }
        assertEquals("Simulated submit enqueue failure.", failure.props[AnalyticsEvents.Params.REASON])
        assertTrue(vm.state.value.message != null)
        assertFalse(vm.state.value.capturing)
    }

    @Test
    fun `the slot follows the queued submit's outbox row and the server's done outranks it`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(penVisit())
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(video())
        val sync = RecordingPenVisitSyncRepository()
        val vm = viewModel(repository, captureSource, sync)
        advanceUntilIdle()

        vm.onEvent(PenVisitDetailEvent.RecordVideo)
        advanceUntilIdle()
        val itemId = "pen-visit-submit-1"

        sync.emitItem(itemId, queueItem(itemId, SyncItemStatus.QUEUED))
        advanceUntilIdle()
        assertEquals(PenVisitVideoState.WORKING, vm.state.value.videoState)

        // A definitive refusal: the queue's own sentence (server farm copy) beside "Record again".
        sync.emitItem(itemId, queueItem(itemId, SyncItemStatus.FAILED, conflict = true, lastError = "The video could not be verified. Record it again."))
        advanceUntilIdle()
        assertEquals(PenVisitVideoState.FAILED, vm.state.value.videoState)
        assertEquals("The video could not be verified. Record it again.", vm.state.value.failureReason)

        // The server's row landing with the verifier outranks whatever the outbox still says:
        // the visit is IN REVIEW, never "done" on submit (maintainer decision 2026-09-12).
        repository.emitDetail(penVisit(status = "pending_verification", canSubmit = false, stateChip = "Visit in review", stateTone = "review", doneLine = "Visited 7 Sep"))
        advanceUntilIdle()
        assertEquals(PenVisitVideoState.IN_REVIEW, vm.state.value.videoState)
        assertEquals("Visited 7 Sep", vm.state.value.doneLine)
        assertEquals(PenVisitTone.REVIEW, vm.state.value.tone)

        // Sent back: the verifier's words, verbatim, and the camera offered again.
        repository.emitDetail(penVisit(status = "rework", canSubmit = true, stateChip = "Visit needs another video", stateTone = "danger", reworkReason = "pen not visible"))
        advanceUntilIdle()
        assertEquals(PenVisitVideoState.REWORK, vm.state.value.videoState)
        assertEquals("pen not visible", vm.state.value.reworkReason)
        assertTrue(vm.state.value.canSubmit)

        // Verified: done, and the parent's work closes on this.
        repository.emitDetail(penVisit(workState = "completed", status = "completed", canSubmit = false, stateChip = "Visit verified", stateTone = "success", doneLine = "Visited 7 Sep · verified 8 Sep", verified = true))
        advanceUntilIdle()
        assertEquals(PenVisitVideoState.DONE, vm.state.value.videoState)
        assertEquals("Visited 7 Sep · verified 8 Sep", vm.state.value.doneLine)
    }

    @Test
    fun `a submit still alive from a previous process reads as sending`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(penVisit())
        val sync = RecordingPenVisitSyncRepository()
        val vm = viewModel(repository, sync = sync)
        advanceUntilIdle()
        assertEquals(PenVisitVideoState.EMPTY, vm.state.value.videoState)

        // Nothing was queued by THIS screen, but the outbox's active set carries the visit's grain.
        sync.emitAliveGrains(setOf(penVisitGrainKey(PEN_VISIT_TEST_TASK_ID)))
        advanceUntilIdle()
        assertEquals(PenVisitVideoState.WORKING, vm.state.value.videoState)
    }

    @Test
    fun `reopening with a durable uploaded proof but no submit row re-enqueues submit`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(penVisit(rowVersion = 11))
        val proofs = FakeProofCaptureRepository().apply {
            seedProofs(
                ProofCaptureRow(
                    id = "proof-recovered",
                    fieldKey = PEN_VISIT_VIDEO_FIELD_KEY,
                    proofSubject = ProofSubject.OTHER,
                    subjectId = PEN_VISIT_TEST_TASK_ID,
                    localUri = "file:///recovered-pen-visit.mp4",
                    processedUri = "file:///processed-recovered-pen-visit.mp4",
                    mimeType = "video/mp4",
                    caption = "Visit Castro 2 · Coimbatore",
                    capturedAtMs = 1_000L,
                    capturedStartMs = 1_000L,
                    capturedEndMs = 9_000L,
                    capturedByPrincipalId = null,
                    syncStatus = CaptureSyncStatus.SYNCED,
                    serverProofId = "server-proof-1",
                    outboxItemId = "proof-outbox-recovered",
                    lastError = null,
                    featureSurface = "pen_visits",
                    featureCategory = "pen_visit",
                    proofMode = "pen_visit_video",
                    processingState = "ATTACHED_TO_SUBMISSION",
                ),
            )
        }
        val sync = RecordingPenVisitSyncRepository()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, sync = sync, proofs = proofs, analytics = analytics)
        advanceUntilIdle()

        val recovered = sync.submits.single()
        assertEquals(PEN_VISIT_TEST_TASK_ID, recovered.taskId)
        assertEquals(11, recovered.rowVersion)
        assertEquals("proof-outbox-recovered", recovered.proofOutboxItemId)
        assertEquals(PenVisitVideoState.WORKING, vm.state.value.videoState)
        assertEquals("file:///processed-recovered-pen-visit.mp4", vm.state.value.previewPath)

        val event = analytics.events.single { it.name == AnalyticsEventsPenVisits.SUBMIT_RECOVERED }
        assertEquals(PEN_VISIT_TEST_TASK_ID, event.props["task_id"])
        assertEquals(PEN_VISIT_VIDEO_FIELD_KEY, event.props[AnalyticsEvents.Params.FIELD])
        assertEquals("proof-recovered", event.props[AnalyticsEvents.Params.PROOF_ID])
        assertEquals("proof-outbox-recovered", event.props[AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])
        assertEquals("pen-visit-submit-1", event.props[AnalyticsEvents.Params.OUTBOX_ITEM_ID])
        assertEquals("11", event.props["row_version"])
        assertEquals("refresh", event.props[AnalyticsEvents.Params.SOURCE])
    }

    private fun queueItem(
        id: String,
        status: SyncItemStatus,
        conflict: Boolean = false,
        lastError: String? = null,
    ) = SyncQueueItem(
        id = id,
        opType = "PEN_VISIT_SUBMIT",
        idempotencyKey = "pen-visit:submit:$PEN_VISIT_TEST_TASK_ID:3",
        groupKey = "pen-visit:task:$PEN_VISIT_TEST_TASK_ID",
        status = status,
        attemptCount = if (status == SyncItemStatus.FAILED) 1 else 0,
        maxAttempts = 5,
        conflict = conflict,
        createdAt = 1L,
        updatedAt = 2L,
        lastError = lastError,
    )

    /**
     * Builds the ViewModel AND keeps a live subscriber on its state.
     *
     * `state` is a `WhileSubscribed` StateFlow, so with no collector it never runs its mapping and
     * every assertion reads the initial empty value — a green-looking test that proves nothing.
     * The collector on `backgroundScope` is what makes these assertions real.
     */
    private fun TestScope.viewModel(
        repository: FakePenVisitsRepository,
        captureSource: FakeProofCaptureSource = FakeProofCaptureSource(),
        sync: RecordingPenVisitSyncRepository = RecordingPenVisitSyncRepository(),
        proofs: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        analytics: RecordingAnalytics = RecordingAnalytics(),
    ): PenVisitDetailViewModel = PenVisitDetailViewModel(
        repository = repository,
        proofCaptureRepository = proofs,
        proofCaptureSource = captureSource,
        syncRepository = sync,
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        appContext = RuntimeEnvironment.getApplication(),
        savedStateHandle = SavedStateHandle(mapOf("task_id" to PEN_VISIT_TEST_TASK_ID)),
    ).also { vm -> backgroundScope.launch { vm.state.collect { } } }

    private fun video() = CapturedVideo(
        localUri = "file:///pen-visit.mp4",
        startedAtMs = 1_000L,
        endedAtMs = 9_000L,
    )

    // ---- Offline, found on the phone 2026-09-25 (server stopped, nothing cached) ------------

    @Test
    fun `with no cached visit and a refresh that never lands the spinner stops and says so`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository(initialDetail = null)
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(video())
        val vm = viewModel(repository, captureSource = captureSource)
        advanceUntilIdle()

        assertFalse("no endless spinner", vm.state.value.loading)
        assertTrue("the screen offers Try again", vm.state.value.unavailable)

        // Record never silently no-ops: the camera stays shut and the tap is answered.
        vm.onEvent(PenVisitDetailEvent.RecordVideo)
        advanceUntilIdle()
        assertEquals(0, captureSource.captureCount)
        assertTrue(vm.state.value.message?.isNotBlank() == true)
    }
}
