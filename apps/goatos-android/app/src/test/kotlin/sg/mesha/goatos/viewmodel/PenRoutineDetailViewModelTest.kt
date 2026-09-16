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
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.CapturedPhoto
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakePhotoCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenRoutines
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.penRoutineGrainKey
import sg.mesha.goatos.feature.penroutines.PenRoutineDetailEvent
import sg.mesha.goatos.feature.penroutines.PenRoutinePhase
import sg.mesha.goatos.feature.penroutines.PenRoutineQuestionKind
import sg.mesha.goatos.feature.penroutines.PenRoutineTone

/**
 * ONE routine task's detail state holder (maintainer instruction 2026-09-16).
 *
 * The rules these hold: every visible word is the backend's; the submit GATE mirrors the
 * server's own rules (required questions, counts at their minimum, presence when required) and
 * never arms a submit the server would refuse; a check-in captures the device's honest location
 * and queues the punch, and `in_pen` flips only from the server's returned task; a real capture
 * reaches the durable slot on the task's own FIFO lane; the server's sentence for a refused
 * write is shown verbatim; a task sent back re-opens the form.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class PenRoutineDetailViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `renders backend copy verbatim and the form from the pinned version`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository(penRoutineTask())
        val vm = viewModel(repository)
        advanceUntilIdle()

        val state = vm.state.value
        assertFalse(state.loading)
        assertEquals("Pen cleaning · Castro 2 · Coimbatore", state.title)
        assertEquals("Castro 2", state.penLabel)
        assertEquals("Every day", state.reasonLine)
        assertEquals("2 questions · 1 photo · check in", state.evidenceLine)
        assertEquals("Due today", state.stateChip)
        assertEquals(PenRoutineTone.INFO, state.tone)
        assertEquals("Check the pen and answer the questions.", state.instruction)
        assertEquals("Check in to the pen before you start", state.presenceLine)
        assertEquals(PenRoutinePhase.OPEN, state.phase)
        assertTrue(state.canCheckIn)
        // The form is the routine's own questions, in order, with the backend's widgets.
        assertEquals(listOf("cleaned", "water", "issues", "count", "note"), state.questions.map { it.id })
        assertEquals(
            listOf(PenRoutineQuestionKind.YES_NO, PenRoutineQuestionKind.CHOICE, PenRoutineQuestionKind.MULTI_CHOICE, PenRoutineQuestionKind.NUMBER, PenRoutineQuestionKind.TEXT),
            state.questions.map { it.kind },
        )
        assertEquals(listOf("Yes", "No"), state.questions.first().options.map { it.label })
        assertEquals("animals", state.questions[3].unit)
        // Slots up to max, required up to min.
        assertEquals(listOf("routine-photo-1", "routine-photo-2"), state.photoSlots.map { it.fieldKey })
        assertEquals(listOf(true, false), state.photoSlots.map { it.required })
        assertEquals(listOf("routine-video-1"), state.videoSlots.map { it.fieldKey })
        assertFalse(state.videoSlots.single().required)
        // Nothing answered, nothing captured, not in the pen: dead Submit.
        assertFalse(state.submitEnabled)
        // Opening the screen re-reads the server's truth once.
        assertEquals(1, repository.refreshDetailCalls)
    }

    @Test
    fun `submit is armed only when every gate the server re-runs is satisfied`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository(penRoutineTask(inPen = false))
        val photos = FakePhotoCaptureSource(mutableListOf<CapturedPhoto?>(photo()))
        val proofs = FakeProofCaptureRepository()
        val vm = viewModel(repository, photoSource = photos, proofs = proofs)
        advanceUntilIdle()

        // 1. Required questions unanswered -> disabled, even with everything else satisfied later.
        vm.onEvent(PenRoutineDetailEvent.CaptureSlot("routine-photo-1"))
        advanceUntilIdle()
        repository.emitDetail(penRoutineTask(inPen = true))
        advanceUntilIdle()
        assertFalse("required questions unanswered", vm.state.value.submitEnabled)

        vm.onEvent(PenRoutineDetailEvent.SetChoice("cleaned", "yes"))
        vm.onEvent(PenRoutineDetailEvent.SetChoice("water", "clean"))
        advanceUntilIdle()
        assertTrue("all satisfied", vm.state.value.submitEnabled)

        // 2. Presence required and not in the pen -> disabled.
        repository.emitDetail(penRoutineTask(inPen = false))
        advanceUntilIdle()
        assertFalse("presence required, not in pen", vm.state.value.submitEnabled)
        repository.emitDetail(penRoutineTask(inPen = true))
        advanceUntilIdle()
        assertTrue(vm.state.value.submitEnabled)

        // 3. A number outside the backend's range -> disabled and marked; back in range -> armed.
        vm.onEvent(PenRoutineDetailEvent.SetText("count", "900"))
        advanceUntilIdle()
        assertFalse(vm.state.value.submitEnabled)
        assertTrue(vm.state.value.questions.single { it.id == "count" }.invalid)
        vm.onEvent(PenRoutineDetailEvent.SetText("count", "3"))
        advanceUntilIdle()
        assertTrue(vm.state.value.submitEnabled)

        // 4. Counts below the minimum -> disabled: a form asking for a video too.
        repository.emitDetail(penRoutineTask(inPen = true, form = penRoutineForm(videoMin = 1)))
        advanceUntilIdle()
        assertFalse("video below min", vm.state.value.submitEnabled)

        // 5. The server's can_submit alone can kill it, whatever the phone holds.
        repository.emitDetail(penRoutineTask(inPen = true, canSubmit = false))
        advanceUntilIdle()
        assertFalse(vm.state.value.submitEnabled)
        assertEquals(PenRoutinePhase.LOCKED, vm.state.value.phase)
    }

    @Test
    fun `presence off needs no check-in and the submit carries the answers in the wire shape`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository(penRoutineTask(presenceRequired = false, canCheckIn = false, rowVersion = 7))
        val photos = FakePhotoCaptureSource(mutableListOf<CapturedPhoto?>(photo()))
        val proofs = FakeProofCaptureRepository()
        val sync = RecordingPenRoutineSyncRepository()
        val facts = FakeClockPunchFactsProvider()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, photoSource = photos, proofs = proofs, sync = sync, facts = facts, analytics = analytics)
        advanceUntilIdle()
        assertFalse(vm.state.value.presenceRequired)

        vm.onEvent(PenRoutineDetailEvent.CaptureSlot("routine-photo-1"))
        vm.onEvent(PenRoutineDetailEvent.SetChoice("cleaned", "no"))
        vm.onEvent(PenRoutineDetailEvent.SetChoice("water", "dirty"))
        vm.onEvent(PenRoutineDetailEvent.ToggleChoice("issues", "limping"))
        vm.onEvent(PenRoutineDetailEvent.ToggleChoice("issues", "coughing"))
        vm.onEvent(PenRoutineDetailEvent.ToggleChoice("issues", "limping"))
        vm.onEvent(PenRoutineDetailEvent.SetText("count", "2"))
        vm.onEvent(PenRoutineDetailEvent.SetText("note", "Gate latch loose"))
        advanceUntilIdle()
        assertTrue(vm.state.value.submitEnabled)

        // The capture landed on ONE FIFO lane per task with the routine's own identity.
        val capture = proofs.captureCalls.single()
        assertEquals("routine-photo-1", capture.fieldKey)
        assertEquals(PEN_ROUTINE_TEST_TASK_ID, capture.subjectId)
        assertEquals("pen-routine:task:$PEN_ROUTINE_TEST_TASK_ID", capture.uploadGroupKey)
        assertEquals("image/jpeg", capture.mimeType)
        assertTrue(vm.state.value.photoSlots.first().previewPath.isNotBlank())

        vm.onEvent(PenRoutineDetailEvent.Submit)
        advanceUntilIdle()

        val queued = sync.submits.single()
        assertEquals(PEN_ROUTINE_TEST_TASK_ID, queued.taskId)
        // The row version the screen rendered rides the submit (the server fences on it).
        assertEquals(7, queued.rowVersion)
        assertEquals(listOf("photo"), queued.proofs.map { it.kind })
        assertTrue(queued.proofs.single().proofOutboxItemId.isNotBlank())
        // yes_no / choice -> the option value, multi_choice -> the values, number -> a number,
        // text -> the text.
        assertEquals("no", queued.answers["cleaned"]?.jsonPrimitive?.content)
        assertEquals("dirty", queued.answers["water"]?.jsonPrimitive?.content)
        assertEquals(JsonArray(listOf(JsonPrimitive("coughing"))), queued.answers["issues"])
        assertEquals(JsonPrimitive(2L), queued.answers["count"])
        assertEquals("Gate latch loose", queued.answers["note"]?.jsonPrimitive?.content)
        // No presence on this routine: no location capture, none on the submit.
        assertEquals(0, facts.captureCount)
        assertNull(queued.location)
        assertEquals(PenRoutinePhase.SENDING, vm.state.value.phase)
        assertTrue(analytics.events.any { it.name == AnalyticsEventsPenRoutines.SUBMIT })
        assertEquals("recorded", analytics.events.single { it.name == AnalyticsEventsPenRoutines.CAPTURE_RESULT }.props[AnalyticsEvents.Params.RESULT])
    }

    @Test
    fun `check in captures the honest location, queues the punch and in_pen flips from the response`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository(penRoutineTask(rowVersion = 5))
        val sync = RecordingPenRoutineSyncRepository()
        val facts = FakeClockPunchFactsProvider()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, sync = sync, facts = facts, analytics = analytics)
        advanceUntilIdle()

        vm.onEvent(PenRoutineDetailEvent.CheckIn)
        advanceUntilIdle()

        assertEquals(1, facts.captureCount)
        val punch = sync.presences.single()
        assertEquals(PEN_ROUTINE_TEST_TASK_ID, punch.taskId)
        assertEquals(5, punch.rowVersion)
        assertEquals("enter", punch.eventType)
        assertEquals("captured", punch.location.status)
        assertEquals(11.0168, punch.location.latitude!!, 0.0001)
        assertEquals(8.0, punch.location.accuracyM!!, 0.0001)
        assertEquals(false, punch.integrity.mockLocation)
        assertEquals(false, punch.integrity.offline)
        assertEquals("queued", analytics.events.single { it.name == AnalyticsEventsPenRoutines.CHECK_IN }.props[AnalyticsEvents.Params.RESULT])

        // While the punch is on the wire the button is down and the line says so.
        val itemId = "pen-routine-presence-1"
        sync.emitItem(itemId, queueItem(itemId, "PEN_ROUTINE_PRESENCE", SyncItemStatus.QUEUED))
        advanceUntilIdle()
        assertTrue(vm.state.value.checkingIn)
        assertFalse(vm.state.value.canCheckIn)
        assertFalse(vm.state.value.inPen)

        // The server's returned task lands in Room: in the pen, from the response and nothing else.
        sync.emitItem(itemId, queueItem(itemId, "PEN_ROUTINE_PRESENCE", SyncItemStatus.SUCCEEDED))
        repository.emitDetail(penRoutineTask(rowVersion = 6, inPen = true, canCheckIn = false))
        advanceUntilIdle()
        assertTrue(vm.state.value.inPen)
        assertFalse(vm.state.value.checkingIn)
        assertEquals("In pen since 07:12", vm.state.value.presenceLine)
    }

    @Test
    fun `a task already in the pen or not offering a check-in queues no punch`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository(penRoutineTask(canCheckIn = false))
        val sync = RecordingPenRoutineSyncRepository()
        val facts = FakeClockPunchFactsProvider()
        val vm = viewModel(repository, sync = sync, facts = facts)
        advanceUntilIdle()

        vm.onEvent(PenRoutineDetailEvent.CheckIn)
        advanceUntilIdle()

        assertTrue(sync.presences.isEmpty())
        assertEquals(0, facts.captureCount)
        // Asking the server again is the honest response to a stale screen.
        assertEquals(2, repository.refreshDetailCalls)
    }

    @Test
    fun `the server's refusal is shown verbatim and a sent-back task re-opens the form`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository(penRoutineTask(presenceRequired = false, canCheckIn = false))
        val photos = FakePhotoCaptureSource(mutableListOf<CapturedPhoto?>(photo(), photo(uri = "file:///retake.jpg", at = 50_000L)))
        val sync = RecordingPenRoutineSyncRepository()
        val vm = viewModel(repository, photoSource = photos, sync = sync)
        advanceUntilIdle()

        vm.onEvent(PenRoutineDetailEvent.CaptureSlot("routine-photo-1"))
        vm.onEvent(PenRoutineDetailEvent.SetChoice("cleaned", "yes"))
        vm.onEvent(PenRoutineDetailEvent.SetChoice("water", "clean"))
        advanceUntilIdle()
        vm.onEvent(PenRoutineDetailEvent.Submit)
        advanceUntilIdle()
        val itemId = "pen-routine-submit-1"
        sync.emitItem(itemId, queueItem(itemId, "PEN_ROUTINE_SUBMIT", SyncItemStatus.QUEUED))
        advanceUntilIdle()
        assertEquals(PenRoutinePhase.SENDING, vm.state.value.phase)
        assertFalse(vm.state.value.submitEnabled)

        // A definitive refusal: the server's own farm sentence beside the re-armed form.
        sync.emitItem(itemId, queueItem(itemId, "PEN_ROUTINE_SUBMIT", SyncItemStatus.FAILED, conflict = true, lastError = "Take one photo of the pen before you submit."))
        advanceUntilIdle()
        assertEquals(PenRoutinePhase.OPEN, vm.state.value.phase)
        assertEquals("Take one photo of the pen before you submit.", vm.state.value.failureReason)
        assertTrue(vm.state.value.submitEnabled)

        // The server's row landing with the verifier outranks whatever the outbox still says.
        repository.emitDetail(penRoutineTask(presenceRequired = false, canCheckIn = false, status = "pending_verification", canSubmit = false, stateChip = "In review", stateTone = "review", doneLine = "Submitted 16 Sep", submittedAt = "2026-09-16T02:00:00Z"))
        advanceUntilIdle()
        assertEquals(PenRoutinePhase.IN_REVIEW, vm.state.value.phase)
        assertEquals("Submitted 16 Sep", vm.state.value.doneLine)
        assertEquals(PenRoutineTone.REVIEW, vm.state.value.tone)

        // Sent back: the verifier's words, verbatim, the form live again, and the OLD capture is
        // history — the slot reads empty until a fresh one is taken.
        repository.emitDetail(penRoutineTask(presenceRequired = false, canCheckIn = false, status = "rework", canSubmit = true, stateChip = "Sent back", stateTone = "danger", reworkReason = "Pen not visible in the photo", submittedAt = "2026-09-16T02:00:00Z", rowVersion = 9))
        advanceUntilIdle()
        assertEquals(PenRoutinePhase.REWORK, vm.state.value.phase)
        assertEquals("Pen not visible in the photo", vm.state.value.reworkReason)
        assertTrue(vm.state.value.photoSlots.first().previewPath.isBlank())
        assertFalse(vm.state.value.submitEnabled)

        vm.onEvent(PenRoutineDetailEvent.CaptureSlot("routine-photo-1"))
        advanceUntilIdle()
        assertTrue(vm.state.value.photoSlots.first().previewPath.isNotBlank())
        assertTrue(vm.state.value.submitEnabled)

        // Done: read-only, the backend's done line.
        repository.emitDetail(penRoutineTask(presenceRequired = false, workState = "completed", status = "completed", canSubmit = false, canCheckIn = false, stateChip = "Done", stateTone = "success", doneLine = "Done 16 Sep · verified 17 Sep", verified = true))
        advanceUntilIdle()
        assertEquals(PenRoutinePhase.DONE, vm.state.value.phase)
        assertEquals("Done 16 Sep · verified 17 Sep", vm.state.value.doneLine)
    }

    @Test
    fun `a cancelled capture writes nothing and a submit still alive from a previous process reads as sending`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository(penRoutineTask(rowVersion = 4))
        val videos = FakeProofCaptureSource().apply { queue(null) }
        val proofs = FakeProofCaptureRepository()
        val sync = RecordingPenRoutineSyncRepository()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, videoSource = videos, proofs = proofs, sync = sync, analytics = analytics)
        advanceUntilIdle()

        vm.onEvent(PenRoutineDetailEvent.CaptureSlot("routine-video-1"))
        advanceUntilIdle()
        assertEquals(1, videos.captureCount)
        assertEquals("Pen cleaning · Castro 2 · Coimbatore", videos.captureContexts.single()?.title)
        assertTrue(proofs.captureCalls.isEmpty())
        assertEquals("cancelled", analytics.events.single { it.name == AnalyticsEventsPenRoutines.CAPTURE_RESULT }.props[AnalyticsEvents.Params.RESULT])
        assertEquals(PenRoutinePhase.OPEN, vm.state.value.phase)

        // Nothing was queued by THIS screen, but the outbox's active set carries the task's grain
        // for the row version on screen.
        sync.emitAliveGrains(setOf(penRoutineGrainKey(PEN_ROUTINE_TEST_TASK_ID, 4)))
        advanceUntilIdle()
        assertEquals(PenRoutinePhase.SENDING, vm.state.value.phase)
    }

    @Test
    fun `draft answers survive a process death through the saved state`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository(penRoutineTask())
        val handle = SavedStateHandle(mapOf("task_id" to PEN_ROUTINE_TEST_TASK_ID))
        val vm = viewModel(repository, handle = handle)
        advanceUntilIdle()
        vm.onEvent(PenRoutineDetailEvent.SetChoice("cleaned", "yes"))
        vm.onEvent(PenRoutineDetailEvent.SetText("note", "Fence ok"))
        advanceUntilIdle()

        // A new ViewModel over the SAME saved state (what the platform restores) sees the draft.
        val restored = viewModel(repository, handle = SavedStateHandle(handle.keys().associateWith { handle.get<Any>(it) }))
        advanceUntilIdle()
        assertEquals(listOf("yes"), restored.state.value.questions.single { it.id == "cleaned" }.selected)
        assertEquals("Fence ok", restored.state.value.questions.single { it.id == "note" }.text)
    }

    private fun queueItem(
        id: String,
        opType: String,
        status: SyncItemStatus,
        conflict: Boolean = false,
        lastError: String? = null,
    ) = SyncQueueItem(
        id = id,
        opType = opType,
        idempotencyKey = "pen-routine:x:$PEN_ROUTINE_TEST_TASK_ID:3",
        groupKey = "pen-routine:task:$PEN_ROUTINE_TEST_TASK_ID",
        status = status,
        attemptCount = if (status == SyncItemStatus.FAILED) 1 else 0,
        maxAttempts = 5,
        conflict = conflict,
        createdAt = 1L,
        updatedAt = 2L,
        lastError = lastError,
    )

    /**
     * Builds the ViewModel AND keeps a live subscriber on its state: `state` is a
     * `WhileSubscribed` StateFlow, so with no collector every assertion reads the initial empty
     * value — a green-looking test that proves nothing.
     */
    private fun TestScope.viewModel(
        repository: FakePenRoutinesRepository,
        videoSource: FakeProofCaptureSource = FakeProofCaptureSource(),
        photoSource: FakePhotoCaptureSource = FakePhotoCaptureSource(),
        sync: RecordingPenRoutineSyncRepository = RecordingPenRoutineSyncRepository(),
        proofs: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        facts: FakeClockPunchFactsProvider = FakeClockPunchFactsProvider(),
        analytics: RecordingAnalytics = RecordingAnalytics(),
        handle: SavedStateHandle = SavedStateHandle(mapOf("task_id" to PEN_ROUTINE_TEST_TASK_ID)),
    ): PenRoutineDetailViewModel = PenRoutineDetailViewModel(
        repository = repository,
        proofCaptureRepository = proofs,
        proofCaptureSource = videoSource,
        photoCaptureSource = photoSource,
        syncRepository = sync,
        factsProvider = facts,
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        appContext = RuntimeEnvironment.getApplication(),
        savedStateHandle = handle,
    ).also { vm -> backgroundScope.launch { vm.state.collect { } } }

    private fun photo(uri: String = "file:///pen-routine.jpg", at: Long = 10_000L) = CapturedPhoto(localUri = uri, capturedAtMs = at)

    @Suppress("unused")
    private fun video() = CapturedVideo(localUri = "file:///pen-routine.mp4", startedAtMs = 1_000L, endedAtMs = 9_000L)
}
