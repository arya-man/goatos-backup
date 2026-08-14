package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.MilkFeedingRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.sync.SyncRepository

/**
 * MOB-003 Proof-flow-integration: Milk feeding completion must not enqueue until mandatory
 * proof steps are enqueued. Double-tap completion must not produce duplicate enqueues.
 *
 * Milk feeding requires evidence (video proof) before submission is possible. This guard test
 * ensures that the completion gate blocks until proof is available and enqueued.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class MilkFeedingSubmitGuardTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `milk feeding completion blocks until proof is available`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val syncRepository = FakeMilkFeedingSyncRepository()

        // Create a minimal ViewModel for milk feeding with no proof available yet
        val vm = MilkFeedingExecuteViewModel(
            repository = FakeMilkFeedingRepository(),
            proofCaptureRepository = proofRepo,
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),  // Empty: no proof
            syncRepository = syncRepository,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            draftRepository = FakeCaptureDraftRepository(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    "taskId" to "milk-task-1",
                    "parkId" to "park-1",
                    "sessionNo" to 1,
                ),
            ),
        )
        advanceUntilIdle()

        // Attempt completion WITHOUT recording proof
        vm.onEvent(sg.mesha.goatos.feature.milk.MilkFeedingEvent.MarkDone)
        advanceUntilIdle()

        // Completion must be blocked: no enqueue
        assertEquals(
            "completion is blocked until proof is recorded",
            0,
            syncRepository.completeKeys.size,
        )
        assertFalse("state must reflect incomplete status", vm.state.value.canComplete)
    }

    @Test
    fun `milk feeding allows completion after proof is recorded`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val syncRepository = FakeMilkFeedingSyncRepository()
        val videoSource = FakeProofCaptureSource(
            mutableListOf(CapturedVideo(localUri = "/proof/milk-feeding.mp4", startedAtMs = 1L, endedAtMs = 2L)),
        )

        val vm = MilkFeedingExecuteViewModel(
            repository = FakeMilkFeedingRepository(),
            proofCaptureRepository = proofRepo,
            proofCaptureSource = videoSource,
            syncRepository = syncRepository,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            draftRepository = FakeCaptureDraftRepository(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    "taskId" to "milk-task-1",
                    "parkId" to "park-1",
                    "sessionNo" to 1,
                ),
            ),
        )
        advanceUntilIdle()

        // Record the mandatory video
        vm.onEvent(sg.mesha.goatos.feature.milk.MilkFeedingEvent.RecordVideo)
        advanceUntilIdle()
        assertEquals("one proof enqueued after recording", 1, proofRepo.captureCalls.size)

        // First completion: must enqueue
        vm.onEvent(sg.mesha.goatos.feature.milk.MilkFeedingEvent.MarkDone)
        advanceUntilIdle()
        assertEquals("one completion enqueue after MarkDone", 1, syncRepository.completeKeys.size)

        // Second completion (double-tap): must NOT produce duplicate enqueue
        vm.onEvent(sg.mesha.goatos.feature.milk.MilkFeedingEvent.MarkDone)
        advanceUntilIdle()
        assertEquals(
            "double-tap completion must not produce duplicate enqueue",
            1,
            syncRepository.completeKeys.size,
        )
    }
}

// Fake implementations for testing
private class FakeMilkFeedingRepository : MilkFeedingRepository {
    override fun observeTaskDetail(taskId: String) = flowOf(
        sg.mesha.goatos.core.data.MilkFeedingTaskDetail(
            taskId = taskId,
            parkId = "park-1",
            parkLabel = "CPT",
            feedingDate = "2026-08-14",
            sessionNo = 1,
            dueTime = "08:00",
            animalCount = 5,
            preparedVolume = 0.0,
            lastError = null,
        ),
    )

    override suspend fun recordPreparedVolume(taskId: String, volumeL: Double) {}
    override suspend fun recordFeeding(taskId: String, count: Int) {}
}

private class FakeMilkFeedingSyncRepository : SyncRepository by FakeSyncRepository() {
    val completeKeys = mutableListOf<String>()

    override suspend fun enqueueMilkFeedingCompletion(request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto, idempotencyKey: String) {
        completeKeys.add(idempotencyKey)
    }
}

private class FakeCaptureDraftRepository : CaptureDraftRepository {
    override suspend fun upsert(draft: sg.mesha.goatos.core.data.CaptureDraft) {}
    override suspend fun get(taskId: String, fieldKey: String) = null
    override suspend fun delete(taskId: String, fieldKey: String) {}
}
