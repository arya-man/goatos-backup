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
import sg.mesha.goatos.core.analytics.AnalyticsEventsToxin
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.feature.toxin.ToxinStepKind
import sg.mesha.goatos.feature.toxin.ToxinStepState
import sg.mesha.goatos.feature.toxin.ToxinTaskDetailEvent
import java.time.Instant

/**
 * The guided round's state holder (module toxin, maintainer decision 2026-08-25).
 *
 * The rule these tests exist to hold: the SERVER owns every step gate. A `waiting` step is not
 * actionable no matter what the device clock says, and its `available_at` is display-only. The
 * second rule: a real capture must reach the durable outbox, referencing its own proof row.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class ToxinTaskDetailViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `maps all four server step states and keeps a waiting step out of reach`() = runTest(dispatcher) {
        // The wait is an HOUR away by the server's own instant, which is exactly the case a device
        // clock could disagree about — and it must still render as blocked.
        val availableAt = Instant.ofEpochMilli(System.currentTimeMillis() + 3_600_000L).toString()
        val repository = FakeToxinRepository(
            toxinDetail(
                steps = listOf(
                    toxinStep(1, state = "done", completedBy = "Amit", completedAt = "2026-08-25T10:40:00Z"),
                    toxinStep(2, state = "available"),
                    toxinStep(4, kind = "wait", state = "waiting", availableAt = availableAt),
                    toxinStep(7, kind = "photo_reading", state = "locked"),
                ),
            ),
        )
        val vm = viewModel(repository)
        advanceUntilIdle()

        val steps = vm.state.value.steps
        assertEquals(listOf(1, 2, 4, 7), steps.map { it.stepNo })
        assertEquals(ToxinStepState.DONE, steps[0].state)
        assertEquals(ToxinStepState.AVAILABLE, steps[1].state)
        assertEquals(ToxinStepState.WAITING, steps[2].state)
        assertEquals(ToxinStepState.LOCKED, steps[3].state)
        assertEquals(ToxinStepKind.WAIT, steps[2].kind)
        assertEquals(ToxinStepKind.PHOTO_READING, steps[3].kind)
        // The attribution line names WHO — steps are person-independent, so this is load-bearing.
        assertTrue(steps[0].completedLine.startsWith("Amit · "))
        // Display-only countdown input is carried; the state is still WAITING.
        assertTrue(steps[2].availableAtEpochMs > 0L)
    }

    @Test
    fun `recording a waiting step captures nothing and enqueues nothing`() = runTest(dispatcher) {
        val availableAt = Instant.ofEpochMilli(System.currentTimeMillis() + 3_600_000L).toString()
        val repository = FakeToxinRepository(
            toxinDetail(steps = listOf(toxinStep(5, state = "waiting", availableAt = availableAt))),
        )
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(video())
        val sync = RecordingToxinSyncRepository()
        val vm = viewModel(repository, captureSource = captureSource, sync = sync)
        advanceUntilIdle()

        vm.onEvent(ToxinTaskDetailEvent.RecordStepVideo(5))
        advanceUntilIdle()

        // The camera never opened and no write was queued: only the server can open a step, and
        // asking it again (the refresh) is the honest response to a stale screen.
        assertEquals(0, captureSource.captureCount)
        assertTrue(sync.stepCompletes.isEmpty())
    }

    @Test
    fun `recording an available step queues the completion against its own proof row`() = runTest(dispatcher) {
        val repository = FakeToxinRepository(
            toxinDetail(steps = listOf(toxinStep(2, state = "available"))),
        )
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(video())
        val sync = RecordingToxinSyncRepository()
        val proofs = FakeProofCaptureRepository()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, captureSource, sync, proofs, analytics)
        advanceUntilIdle()

        vm.onEvent(ToxinTaskDetailEvent.RecordStepVideo(2))
        advanceUntilIdle()

        assertEquals(1, captureSource.captureCount)
        assertEquals(1, sync.stepCompletes.size)
        val queued = sync.stepCompletes.single()
        assertEquals(TOXIN_TEST_TASK_ID, queued.taskId)
        assertEquals(2, queued.stepNo)
        assertTrue(queued.proofOutboxItemId.isNotBlank())
        // One FIFO lane per round, so the upload always drains before this completion.
        assertEquals("toxin:task:$TOXIN_TEST_TASK_ID", proofs.captureCalls.single().uploadGroupKey)
        // One capture identity per step, so a re-shoot replaces rather than accumulates.
        assertEquals("step-2", proofs.captureCalls.single().fieldKey)

        assertTrue(analytics.events.any { it.name == AnalyticsEventsToxin.TOXIN_STEP_VIDEO_CAPTURED })
        val submitted = analytics.events.single { it.name == AnalyticsEventsToxin.TOXIN_STEP_SUBMITTED }
        assertEquals("2", submitted.props[AnalyticsEventsToxin.Params.STEP_NO])
    }

    @Test
    fun `the reading needs both the strip photo and an outcome before it can be sent`() = runTest(dispatcher) {
        val repository = FakeToxinRepository(
            toxinDetail(steps = listOf(toxinStep(7, kind = "photo_reading", state = "available"))),
        )
        val photoSource = AlwaysCapturingPhotoSource()
        val sync = RecordingToxinSyncRepository()
        val proofs = FakeProofCaptureRepository()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, sync = sync, proofs = proofs, analytics = analytics, photoSource = photoSource)
        advanceUntilIdle()

        // Backend-owned vocabulary reached the screen verbatim.
        assertEquals(listOf("Negative", "Positive", "Invalid strip"), vm.state.value.outcomeOptions.map { it.label })

        // Outcome alone is not enough — a reading with no strip photo proves nothing.
        vm.onEvent(ToxinTaskDetailEvent.SelectOutcome("negative"))
        advanceUntilIdle()
        assertFalse(vm.state.value.submitEnabled)
        vm.onEvent(ToxinTaskDetailEvent.SubmitReading)
        advanceUntilIdle()
        assertTrue(sync.submits.isEmpty())

        vm.onEvent(ToxinTaskDetailEvent.CaptureStripPhoto)
        advanceUntilIdle()
        assertEquals(1, photoSource.captureCount)
        assertTrue(vm.state.value.stripPhotoCaptured)
        assertTrue(vm.state.value.submitEnabled)
        assertEquals("strip-photo", proofs.captureCalls.single().fieldKey)
        assertTrue(analytics.events.any { it.name == AnalyticsEventsToxin.TOXIN_STRIP_PHOTO_CAPTURED })

        vm.onEvent(ToxinTaskDetailEvent.SubmitReading)
        advanceUntilIdle()

        val submit = sync.submits.single()
        assertEquals(TOXIN_TEST_TASK_ID, submit.taskId)
        // The BACKEND's option value, never its label and never a client-invented token.
        assertEquals("negative", submit.outcome)
        assertTrue(submit.stripPhotoOutboxItemId.isNotBlank())
        assertTrue(vm.state.value.submitQueued)
        val tracked = analytics.events.single { it.name == AnalyticsEventsToxin.TOXIN_READING_SUBMITTED }
        assertEquals("negative", tracked.props[AnalyticsEvents.Params.OUTCOME])
    }

    @Test
    fun `a failed step enqueue reports the real reason and leaves the screen usable`() = runTest(dispatcher) {
        val repository = FakeToxinRepository(
            toxinDetail(steps = listOf(toxinStep(3, state = "available"))),
        )
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(video())
        val sync = RecordingToxinSyncRepository().apply { failNextStepComplete = true }
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, captureSource, sync, analytics = analytics)
        advanceUntilIdle()

        vm.onEvent(ToxinTaskDetailEvent.RecordStepVideo(3))
        advanceUntilIdle()

        assertTrue(sync.stepCompletes.isEmpty())
        val failure = analytics.events.single { it.name == AnalyticsEventsToxin.TOXIN_FAILURE }
        // The REAL message the repository returned, never a fabricated code.
        assertEquals("Simulated step enqueue failure.", failure.props[AnalyticsEvents.Params.REASON])
        assertTrue(vm.state.value.message != null)
        // The step is not stuck "working" — the operator can try again.
        assertFalse(vm.state.value.steps.single().working)
    }

    /**
     * Builds the ViewModel AND keeps a live subscriber on its state.
     *
     * `state` is a `WhileSubscribed` StateFlow, so with no collector it never runs its mapping and
     * every assertion reads the initial empty value — a green-looking test that proves nothing.
     * The collector on `backgroundScope` is what makes these assertions real.
     */
    private fun TestScope.viewModel(
        repository: FakeToxinRepository,
        captureSource: FakeProofCaptureSource = FakeProofCaptureSource(),
        sync: RecordingToxinSyncRepository = RecordingToxinSyncRepository(),
        proofs: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        analytics: RecordingAnalytics = RecordingAnalytics(),
        photoSource: AlwaysCapturingPhotoSource = AlwaysCapturingPhotoSource(),
    ): ToxinTaskDetailViewModel = ToxinTaskDetailViewModel(
        repository = repository,
        proofCaptureRepository = proofs,
        proofCaptureSource = captureSource,
        photoCaptureSource = photoSource,
        syncRepository = sync,
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        appContext = RuntimeEnvironment.getApplication(),
        savedStateHandle = SavedStateHandle(mapOf("task_id" to TOXIN_TEST_TASK_ID)),
    ).also { vm -> backgroundScope.launch { vm.state.collect { } } }

    private fun video() = CapturedVideo(
        localUri = "file:///step.mp4",
        startedAtMs = 1_000L,
        endedAtMs = 9_000L,
    )
}
