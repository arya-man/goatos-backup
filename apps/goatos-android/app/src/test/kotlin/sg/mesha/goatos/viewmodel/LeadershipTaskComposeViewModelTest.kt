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
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEventsLeadershipTasks
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.network.dto.LeadershipAssigneeDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskAttachmentDto
import sg.mesha.goatos.feature.leadershiptasks.LEADERSHIP_TASK_ATTACHMENT_CAP
import sg.mesha.goatos.feature.leadershiptasks.LeadershipAttachmentKind
import sg.mesha.goatos.feature.leadershiptasks.LeadershipDraftUploadState
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskComposeEvent

/**
 * The raise/edit form's state holder (maintainer request 2026-09-04).
 *
 * The rules these hold: every idempotency key is minted ONCE per draft and reused verbatim on
 * a retry (task and attachment alike), a failed send keeps the draft and its uploaded proof ids,
 * the attachment cap is the last word, and the send opens only with a title and an assignee.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class LeadershipTaskComposeViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(
        repository: FakeLeadershipTasksRepository,
        recorder: FakeVoiceNoteRecorder = FakeVoiceNoteRecorder(),
        importer: FakeAttachmentImporter = FakeAttachmentImporter(),
        analytics: RecordingAnalytics = RecordingAnalytics(),
        savedStateHandle: SavedStateHandle = SavedStateHandle(),
    ) = LeadershipTaskComposeViewModel(
        repository = repository,
        recorder = recorder,
        importer = importer,
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        appContext = RuntimeEnvironment.getApplication(),
        savedStateHandle = savedStateHandle,
    )

    @Test
    fun `send opens only with a title and an assignee, and a lone assignee is preselected`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository(assigneeList = listOf(LeadershipAssigneeDto(userId = "cxo-1", name = "Ravi")))
        val vm = viewModel(repository)
        val job = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals("cxo-1", vm.state.value.selectedAssigneeId)
        assertFalse("no title, no send", vm.state.value.canSend)
        vm.onEvent(LeadershipTaskComposeEvent.TitleChanged("Fix the water line"))
        assertTrue(vm.state.value.canSend)
        job.cancel()
    }

    @Test
    fun `the task key is minted once per draft and reused verbatim on a retry`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository(assigneeList = listOf(LeadershipAssigneeDto(userId = "cxo-1", name = "Ravi")))
        repository.failNextRaise = true
        val handle = SavedStateHandle()
        val vm = viewModel(repository, savedStateHandle = handle)
        val job = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        vm.onEvent(LeadershipTaskComposeEvent.TitleChanged("Fix the water line"))
        vm.onEvent(LeadershipTaskComposeEvent.BodyChanged("Before the weekend."))

        vm.onEvent(LeadershipTaskComposeEvent.Send)
        advanceUntilIdle()
        assertEquals(1, repository.raises.size)
        assertNull("a failed send never leaves the form", vm.state.value.sentTaskId)
        assertEquals("the draft survives the failure", "Fix the water line", vm.state.value.title)
        assertTrue(vm.state.value.message?.isNotBlank() == true)

        vm.onEvent(LeadershipTaskComposeEvent.Send)
        advanceUntilIdle()
        assertEquals(2, repository.raises.size)
        assertEquals(repository.raises[0].idempotencyKey, repository.raises[1].idempotencyKey)
        assertEquals(vm.taskIdempotencyKey, repository.raises[1].idempotencyKey)
        // The key survives process death through the SavedStateHandle.
        assertEquals(vm.taskIdempotencyKey, handle.get<String>(LeadershipTaskComposeViewModel.KEY_TASK_IDEMPOTENCY))
        assertEquals("task-new", vm.state.value.sentTaskId)
        assertEquals("cxo-1", repository.raises[1].request.assigneeUserId)
        job.cancel()
    }

    @Test
    fun `attachments upload under their own keys, keep their proof ids across a retry, and ride the request in order`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository(assigneeList = listOf(LeadershipAssigneeDto(userId = "cxo-1", name = "Ravi")))
        val recorder = FakeVoiceNoteRecorder()
        val analytics = RecordingAnalytics()
        val vm = viewModel(repository, recorder = recorder, analytics = analytics)
        val job = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        vm.onEvent(LeadershipTaskComposeEvent.TitleChanged("Pump photos"))

        vm.onEvent(LeadershipTaskComposeEvent.StartRecording)
        assertNotNull("recording shows its clock", vm.state.value.recordingElapsedMs)
        assertFalse("no send mid-recording", vm.state.value.canSend)
        vm.onEvent(LeadershipTaskComposeEvent.StopRecording)
        vm.onEvent(LeadershipTaskComposeEvent.MediaPicked(listOf("content://media/pump.jpg", "content://media/leak.mp4")))
        vm.onEvent(LeadershipTaskComposeEvent.FilesPicked(listOf("content://docs/quote.pdf")))
        advanceUntilIdle()

        val kinds = vm.state.value.attachments.map { it.kind }
        assertEquals(
            listOf(LeadershipAttachmentKind.AUDIO, LeadershipAttachmentKind.PHOTO, LeadershipAttachmentKind.VIDEO, LeadershipAttachmentKind.FILE),
            kinds,
        )
        assertEquals("0:04", vm.state.value.attachments[0].durationLabel)
        assertTrue(analytics.events.any { it.name == AnalyticsEventsLeadershipTasks.AUDIO_RECORDED })
        assertEquals(4, analytics.events.count { it.name == AnalyticsEventsLeadershipTasks.ATTACHMENT_ADDED })
        val keysBefore = vm.state.value.attachments.map { it.listKey }
        assertEquals("four distinct attachment keys", 4, keysBefore.toSet().size)

        // A happy send: every local attachment uploads under its OWN key, in order.
        vm.onEvent(LeadershipTaskComposeEvent.Send)
        advanceUntilIdle()
        assertEquals(keysBefore, repository.uploads.map { it.idempotencyKey })
        assertEquals(listOf("proof-1", "proof-2", "proof-3", "proof-4"), repository.raises.single().request.attachments.map { it.proofId })
        assertEquals("task-new", vm.state.value.sentTaskId)
        job.cancel()

        // A send that fails mid-way keeps every uploaded proof id and every key; the retry re-sends
        // the SAME keys and never re-mints.
        val repo2 = FakeLeadershipTasksRepository(assigneeList = listOf(LeadershipAssigneeDto(userId = "cxo-1", name = "Ravi")))
        val vm2 = viewModel(repo2)
        val job2 = backgroundScope.launch { vm2.state.collect {} }
        advanceUntilIdle()
        vm2.onEvent(LeadershipTaskComposeEvent.TitleChanged("Pump photos"))
        vm2.onEvent(LeadershipTaskComposeEvent.MediaPicked(listOf("content://media/a.jpg", "content://media/b.jpg")))
        advanceUntilIdle()
        val keys = vm2.state.value.attachments.map { it.listKey }
        repo2.failNextUpload = true
        vm2.onEvent(LeadershipTaskComposeEvent.Send)
        advanceUntilIdle()
        assertEquals("first upload failed, second never attempted", 1, repo2.uploads.size)
        assertEquals(keys[0], repo2.uploads[0].idempotencyKey)
        assertTrue("no task request after a failed upload", repo2.raises.isEmpty())
        assertEquals(LeadershipDraftUploadState.FAILED, vm2.state.value.attachments[0].uploadState)

        vm2.onEvent(LeadershipTaskComposeEvent.Send)
        advanceUntilIdle()
        assertEquals(3, repo2.uploads.size)
        assertEquals("the retry re-sends the SAME attachment key", keys[0], repo2.uploads[1].idempotencyKey)
        assertEquals(keys[1], repo2.uploads[2].idempotencyKey)
        val request = repo2.raises.single().request
        assertEquals(listOf("proof-1", "proof-2"), request.attachments.map { it.proofId })
        assertEquals(listOf("photo", "photo"), request.attachments.map { it.kind })
        assertEquals("task-new", vm2.state.value.sentTaskId)
        job2.cancel()
    }

    @Test
    fun `the attachment cap is the last word and an oversized file is refused`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository(assigneeList = listOf(LeadershipAssigneeDto(userId = "cxo-1", name = "Ravi")))
        val importer = FakeAttachmentImporter(tooLarge = setOf("content://docs/huge.bin"))
        val vm = viewModel(repository, importer = importer)
        val job = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(LeadershipTaskComposeEvent.FilesPicked(listOf("content://docs/huge.bin")))
        advanceUntilIdle()
        assertTrue(vm.state.value.attachments.isEmpty())
        assertTrue("too-large gets a farm sentence", vm.state.value.message?.isNotBlank() == true)

        vm.onEvent(LeadershipTaskComposeEvent.DismissMessage)
        vm.onEvent(LeadershipTaskComposeEvent.MediaPicked((1..LEADERSHIP_TASK_ATTACHMENT_CAP + 3).map { "content://media/p$it.jpg" }))
        advanceUntilIdle()
        assertEquals(LEADERSHIP_TASK_ATTACHMENT_CAP, vm.state.value.attachments.size)
        assertTrue("the cap says so", vm.state.value.message?.isNotBlank() == true)

        // A recording past the cap is refused before the microphone opens.
        vm.onEvent(LeadershipTaskComposeEvent.StartRecording)
        assertNull(vm.state.value.recordingElapsedMs)
        job.cancel()
    }

    @Test
    fun `edit seeds the draft from the stored task, locks the assignee, and sends the full attachment list`() = runTest(dispatcher) {
        val stored = leadershipTask(
            isSeen = true,
            canEdit = true,
            rowVersion = 9,
            attachments = listOf(
                LeadershipTaskAttachmentDto(attachmentId = "att-1", proofId = "proof-kept", kind = "file", mimeType = "application/pdf", fileName = "quote.pdf", sizeBytes = 10L, position = 0),
            ),
        )
        val repository = FakeLeadershipTasksRepository(initialDetail = stored)
        val vm = viewModel(repository, savedStateHandle = SavedStateHandle(mapOf("task_id" to LEADERSHIP_TEST_TASK_ID)))
        val job = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(vm.state.value.isEdit)
        assertEquals("Fix the CPT water line", vm.state.value.title)
        assertEquals("cxo-1", vm.state.value.selectedAssigneeId)
        assertEquals(listOf("Ravi"), vm.state.value.assignees.map { it.name })
        assertEquals(LeadershipDraftUploadState.UPLOADED, vm.state.value.attachments.single().uploadState)

        vm.onEvent(LeadershipTaskComposeEvent.SelectAssignee("someone-else"))
        assertEquals("the assignee is locked on edit", "cxo-1", vm.state.value.selectedAssigneeId)

        vm.onEvent(LeadershipTaskComposeEvent.TitleChanged("Fix the CPT water line today"))
        vm.onEvent(LeadershipTaskComposeEvent.MediaPicked(listOf("content://media/new.jpg")))
        advanceUntilIdle()
        vm.onEvent(LeadershipTaskComposeEvent.Send)
        advanceUntilIdle()

        val edit = repository.edits.single()
        assertEquals(LEADERSHIP_TEST_TASK_ID, edit.taskId)
        assertEquals(9, edit.request.rowVersion)
        assertEquals("a stored attachment is not re-uploaded", 1, repository.uploads.size)
        assertEquals(listOf("proof-kept", "proof-1"), edit.request.attachments.map { it.proofId })
        assertEquals(LEADERSHIP_TEST_TASK_ID, vm.state.value.sentTaskId)
        job.cancel()
    }
}
