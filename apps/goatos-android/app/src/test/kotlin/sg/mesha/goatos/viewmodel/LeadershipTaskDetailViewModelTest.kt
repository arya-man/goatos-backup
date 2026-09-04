package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
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
import sg.mesha.goatos.boot.NavStateRefreshSignal
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEventsLeadershipTasks
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.network.dto.LeadershipTaskAttachmentDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskStatusOptionDto
import sg.mesha.goatos.feature.leadershiptasks.LeadershipAttachmentKind
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskDetailEvent

/**
 * ONE task's detail state holder (maintainer request 2026-09-04).
 *
 * The rules these hold: `seen` fires ONCE and only for an unseen task the caller can act on; a
 * status write carries the row version on screen and mints its key ONCE, reusing it verbatim on
 * the retry; every visible word is the backend's.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class LeadershipTaskDetailViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(
        repository: FakeLeadershipTasksRepository,
        analytics: RecordingAnalytics = RecordingAnalytics(),
        signal: NavStateRefreshSignal = NavStateRefreshSignal(),
    ) = LeadershipTaskDetailViewModel(
        repository = repository,
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        navRefresh = signal,
        appContext = RuntimeEnvironment.getApplication(),
        savedStateHandle = SavedStateHandle(mapOf("task_id" to LEADERSHIP_TEST_TASK_ID)),
    )

    @Test
    fun `seen fires once for an unseen assigned task and re-reads the badge`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository(initialDetail = leadershipTask(isSeen = false, canChangeStatus = true))
        val signal = NavStateRefreshSignal()
        val refreshRequests = mutableListOf<Unit>()
        val signalJob = backgroundScope.launch { signal.requests.collect { refreshRequests += it } }
        val vm = viewModel(repository, signal = signal)
        val stateJob = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(listOf(LEADERSHIP_TEST_TASK_ID), repository.seenCalls)
        assertEquals("seen success re-reads the badge", 1, refreshRequests.size)

        // A second emission of the same task (a refresh landing) must NOT fire seen again.
        repository.emitDetail(leadershipTask(isSeen = true, canChangeStatus = true).copy(rowVersion = 4))
        advanceUntilIdle()
        assertEquals(1, repository.seenCalls.size)

        signalJob.cancel()
        stateJob.cancel()
    }

    @Test
    fun `seen never fires for a task already seen or for the raiser`() = runTest(dispatcher) {
        listOf(
            leadershipTask(isSeen = true, canChangeStatus = true),
            leadershipTask(isSeen = false, canChangeStatus = false, canEdit = true, statusOptions = emptyList()),
        ).forEach { task ->
            val repository = FakeLeadershipTasksRepository(initialDetail = task)
            val vm = viewModel(repository)
            val job = backgroundScope.launch { vm.state.collect {} }
            advanceUntilIdle()
            assertTrue("no seen for seen=${task.isSeen} canChangeStatus=${task.canChangeStatus}", repository.seenCalls.isEmpty())
            job.cancel()
        }
    }

    @Test
    fun `a status change carries the row version and keeps one key across a retry`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository(initialDetail = leadershipTask(isSeen = true, rowVersion = 7))
        repository.failNextStatus = true
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, analytics)
        val job = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(LeadershipTaskDetailEvent.ChangeStatus("in_progress"))
        advanceUntilIdle()
        // First attempt failed: the draft shows the connection line and the task did not move.
        assertEquals(1, repository.statusCalls.size)
        assertEquals(7, repository.statusCalls[0].request.rowVersion)
        assertEquals("in_progress", repository.statusCalls[0].request.status)
        assertTrue(vm.state.value.message?.isNotBlank() == true)
        assertTrue(analytics.events.any { it.name == AnalyticsEventsLeadershipTasks.FAILURE })

        vm.onEvent(LeadershipTaskDetailEvent.ChangeStatus("in_progress"))
        advanceUntilIdle()
        assertEquals(2, repository.statusCalls.size)
        assertEquals(
            "a retry must reuse the SAME idempotency key so the server cannot move the task twice",
            repository.statusCalls[0].idempotencyKey,
            repository.statusCalls[1].idempotencyKey,
        )
        assertTrue(analytics.events.any { it.name == AnalyticsEventsLeadershipTasks.STATUS_CHANGED })
        assertEquals("the retry carried the same target", "in_progress", repository.statusCalls[1].request.status)

        job.cancel()
    }

    @Test
    fun `cancel is confirmed first and then sent as the cancelled status`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository(
            initialDetail = leadershipTask(isSeen = true, canChangeStatus = false, canEdit = true, canCancel = true, statusOptions = emptyList()),
        )
        val vm = viewModel(repository)
        val job = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(vm.state.value.canCancel)
        assertTrue(vm.state.value.statusOptions.isEmpty())
        vm.onEvent(LeadershipTaskDetailEvent.RequestCancel)
        assertTrue(vm.state.value.showCancelConfirm)
        assertTrue("asking is not sending", repository.statusCalls.isEmpty())

        vm.onEvent(LeadershipTaskDetailEvent.ConfirmCancel)
        advanceUntilIdle()
        assertFalse(vm.state.value.showCancelConfirm)
        assertEquals("cancelled", repository.statusCalls.single().request.status)

        job.cancel()
    }

    @Test
    fun `the detail renders backend copy verbatim and opens an attachment through the cache`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository(
            initialDetail = leadershipTask(
                isSeen = true,
                statusOptions = listOf(LeadershipTaskStatusOptionDto(key = "done", label = "Mark done")),
                attachments = listOf(
                    LeadershipTaskAttachmentDto(attachmentId = "att-1", proofId = "proof-1", kind = "photo", mimeType = "image/jpeg", fileName = "pump.jpg", sizeBytes = 2_048L, position = 0),
                    LeadershipTaskAttachmentDto(attachmentId = "att-2", proofId = "proof-2", kind = "audio", mimeType = "audio/mp4", fileName = "Voice note 1", sizeBytes = 0L, durationMs = 61_000L, position = 1),
                ),
            ),
        )
        val vm = viewModel(repository)
        val job = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("#12", state.numberLabel)
        assertEquals("Open", state.statusChip)
        assertEquals("Raised by Hemant · 4 Sep 2026", state.metaLine)
        assertEquals(listOf("Mark done"), state.statusOptions.map { it.label })
        assertEquals(listOf(LeadershipAttachmentKind.PHOTO, LeadershipAttachmentKind.AUDIO), state.attachments.map { it.kind })
        assertEquals("1:01", state.attachments[1].durationLabel)
        assertEquals("2 KB", state.attachments[0].sizeLabel)

        vm.onEvent(LeadershipTaskDetailEvent.OpenAttachment("att-1"))
        advanceUntilIdle()
        assertEquals(listOf("proof-1"), repository.fetchedProofs)
        assertEquals(listOf(FakeLeadershipTasksRepository.FetchCall(LEADERSHIP_TEST_TASK_ID, "proof-1")), repository.fetchCalls)
        assertEquals("/cache/proof-1", vm.state.value.attachments[0].localPath)

        job.cancel()
    }
}
