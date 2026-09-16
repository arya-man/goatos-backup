package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedPhoto
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakePhotoCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.WorkflowVideoDraft
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.data.sync.WorkflowProofOutboxRef
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowOverdueDateDto
import sg.mesha.goatos.core.network.dto.WorkflowSubjectDto
import sg.mesha.goatos.feature.counts.WorkflowDetailEvent

/**
 * Death follows the SOP (docs/decisions/sop-driven-herd-operations.md): the operator records every
 * authored step on the phone -- videos, photos and answers -- and ONE Submit sends each step. The
 * seeded two-video Death SOP must send exactly the two completions it sends today.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WorkflowDetailViewModelDeathFollowsSopTest {
    private val dispatcher = StandardTestDispatcher()
    private lateinit var videos: FakeProofCaptureSource
    private lateinit var photos: FakePhotoCaptureSource
    private lateinit var sync: RecordingWorkflowSync
    private lateinit var saved: SavedStateHandle

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        videos = FakeProofCaptureSource()
        photos = FakePhotoCaptureSource()
        sync = RecordingWorkflowSync()
        saved = SavedStateHandle(mapOf(WorkflowDetailViewModel.ARG_WORKFLOW_ID to WORKFLOW_ID))
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(repo: DraftingWorkflowsRepository) = WorkflowDetailViewModel(
        repo = repo,
        syncRepository = sync,
        proofCaptureSource = videos,
        proofCaptureRepository = FakeProofCaptureRepository(),
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
        savedStateHandle = saved,
        photoCaptureSource = photos,
        countsRepository = FakeAddCountsRepository(),
    )

    private fun queueVideo(at: Long) = videos.queue(CapturedVideo(localUri = "file:///v$at.mp4", startedAtMs = at, endedAtMs = at + 4_000))

    @Test
    fun `seeded two-video death submits exactly today's two video completions`() = runTest(dispatcher) {
        val repo = DraftingWorkflowsRepository(death(videoStep("a-death", "death_video", 1), videoStep("a-pm", "post_mortem_video", 2)))
        val vm = viewModel(repo)
        advanceUntilIdle()

        queueVideo(1_000)
        vm.onEvent(WorkflowDetailEvent.RecordVideo("a-death"))
        advanceUntilIdle()
        assertFalse("one of two videos recorded", vm.state.value.deathSubmissionEnabled)
        queueVideo(2_000)
        vm.onEvent(WorkflowDetailEvent.RecordVideo("a-pm"))
        advanceUntilIdle()
        assertTrue(vm.state.value.deathSubmissionEnabled)
        assertEquals("the seeded drafts keep the bare action-id slot", listOf("a-death", "a-pm"), repo.drafts.value.map { it.fieldKey })

        vm.onEvent(WorkflowDetailEvent.SubmitDeath)
        advanceUntilIdle()

        assertTrue("a seeded death sends no answers", sync.answers.isEmpty())
        assertEquals(listOf("a-death", "a-pm"), sync.completes.map { it.actionId })
        sync.completes.forEach { call ->
            val proofId = call.proofOutboxItemId ?: error("a seeded video completion carries its one proof id")
            assertEquals(workflowVideoCompletionKey(call.actionId, proofId), call.idempotencyKey)
            assertTrue(call.proofOutboxItems.isEmpty())
        }
    }

    @Test
    fun `an authored death holds Submit until every step is recorded and sends each step`() = runTest(dispatcher) {
        val repo = DraftingWorkflowsRepository(
            death(
                videoStep("s1", "carcass_video", 1),
                WorkflowActionDto(actionId = "s2", actionKey = "wound_photos", seq = 2, section = "main", actionType = "action", title = "Wound photos", status = "pending", proofMinPhotos = 2),
                WorkflowActionDto(actionId = "s3", actionKey = "vet_called", seq = 3, section = "main", actionType = "question", answerType = "yes_no", title = "Vet called?", status = "pending"),
            ),
        )
        val vm = viewModel(repo)
        advanceUntilIdle()

        queueVideo(1_000)
        vm.onEvent(WorkflowDetailEvent.RecordVideo("s1"))
        advanceUntilIdle()
        photos.queue(CapturedPhoto(localUri = "file:///p1.jpg", capturedAtMs = 2_000))
        vm.onEvent(WorkflowDetailEvent.TakePhoto("s2"))
        advanceUntilIdle()
        assertFalse("one of two photos, and the question unanswered", vm.state.value.deathSubmissionEnabled)
        photos.queue(CapturedPhoto(localUri = "file:///p2.jpg", capturedAtMs = 3_000))
        vm.onEvent(WorkflowDetailEvent.TakePhoto("s2"))
        advanceUntilIdle()
        assertFalse("the question is still unanswered", vm.state.value.deathSubmissionEnabled)
        assertTrue("answering a death question sends nothing yet", sync.answers.isEmpty())

        vm.onEvent(WorkflowDetailEvent.Answer("s3", "yes"))
        advanceUntilIdle()
        assertTrue(sync.answers.isEmpty())
        assertEquals(3, vm.state.value.actionsDone)
        assertTrue(vm.state.value.deathSubmissionEnabled)

        vm.onEvent(WorkflowDetailEvent.SubmitDeath)
        advanceUntilIdle()

        assertEquals(listOf("s1", "s2"), sync.completes.map { it.actionId })
        assertTrue("a one-video step keeps its single proof id", sync.completes[0].proofOutboxItemId != null)
        assertEquals(listOf("photo", "photo"), sync.completes[1].proofOutboxItems.map { it.kind })
        val answer = sync.answers.single()
        assertEquals("s3", answer.actionId)
        assertEquals("yes", answer.answerValue)
        assertEquals(3, saved.get<Int>("workflowDetail.deathSubmittedSteps"))
    }

    @Test
    fun `the death reads as submitted only when every step write is confirmed`() = runTest(dispatcher) {
        val repo = DraftingWorkflowsRepository(
            death(
                videoStep("s1", "carcass_video", 1),
                WorkflowActionDto(actionId = "s2", actionKey = "vet_called", seq = 2, section = "main", actionType = "question", answerType = "yes_no", title = "Vet called?", status = "pending"),
                videoStep("s3", "burial_video", 3),
            ),
        )
        val vm = viewModel(repo)
        advanceUntilIdle()
        queueVideo(1_000)
        vm.onEvent(WorkflowDetailEvent.RecordVideo("s1"))
        advanceUntilIdle()
        vm.onEvent(WorkflowDetailEvent.Answer("s2", "no"))
        queueVideo(2_000)
        vm.onEvent(WorkflowDetailEvent.RecordVideo("s3"))
        advanceUntilIdle()
        vm.onEvent(WorkflowDetailEvent.SubmitDeath)
        advanceUntilIdle()

        sync.succeed(count = 2)
        advanceUntilIdle()
        assertFalse("two of three step writes is not submitted", vm.state.value.returnToList)
        assertTrue(repo.drafts.value.isNotEmpty())

        sync.succeed(count = 3)
        advanceUntilIdle()
        assertTrue(vm.state.value.returnToList)
        assertTrue(repo.drafts.value.isEmpty())
        assertTrue(repo.answers.value.isEmpty())
        assertNull(saved.get<Int>("workflowDetail.deathSubmittedSteps"))
    }

    @Test
    fun `an answer-only death finalizes on its confirmed step writes though it holds no drafts`() = runTest(dispatcher) {
        val repo = DraftingWorkflowsRepository(
            death(
                WorkflowActionDto(actionId = "q1", actionKey = "vet_called", seq = 1, section = "main", actionType = "question", answerType = "yes_no", title = "Vet called?", status = "pending"),
                WorkflowActionDto(actionId = "q2", actionKey = "buried", seq = 2, section = "main", actionType = "question", answerType = "yes_no", title = "Buried?", status = "pending"),
            ),
        )
        val vm = viewModel(repo)
        advanceUntilIdle()
        vm.onEvent(WorkflowDetailEvent.Answer("q1", "yes"))
        vm.onEvent(WorkflowDetailEvent.Answer("q2", "no"))
        advanceUntilIdle()
        assertTrue(vm.state.value.deathSubmissionEnabled)

        vm.onEvent(WorkflowDetailEvent.SubmitDeath)
        advanceUntilIdle()
        assertEquals(listOf("q1", "q2"), sync.answers.map { it.actionId })
        assertTrue("an answer-only death has no video drafts to mark", repo.drafts.value.isEmpty())
        assertFalse("a sent answer-only death cannot be sent again while it is in flight", vm.state.value.deathSubmissionEnabled)

        sync.succeed(count = 1)
        advanceUntilIdle()
        assertFalse("one of two answers confirmed is not submitted", vm.state.value.returnToList)

        sync.succeed(count = 2)
        advanceUntilIdle()
        assertTrue("every answer confirmed: the death reads as submitted", vm.state.value.returnToList)
        assertTrue(repo.answers.value.isEmpty())
        assertNull(saved.get<Int>("workflowDetail.deathSubmittedSteps"))
    }

    private fun videoStep(id: String, key: String, seq: Int) = WorkflowActionDto(
        actionId = id, actionKey = key, seq = seq, section = "main", actionType = "action",
        title = key, requiresVideo = true, status = "pending",
        blocked = seq > 1, blockedReason = if (seq > 1) "previous_action" else "",
    )

    private fun death(vararg actions: WorkflowActionDto) = WorkflowDetailResponseDto(
        workflowId = WORKFLOW_ID,
        module = "death",
        templateKey = "death",
        subject = WorkflowSubjectDto(goatId = GOAT_ID, displayId = "GOAT-1"),
        actions = actions.toList(),
    )

    private companion object {
        const val WORKFLOW_ID = "wf-death-sop"
        const val GOAT_ID = "44444444-4444-4444-4444-444444444444"
    }
}

/** Room stand-in holding the death drafts and draft answers the way the DAO does. */
private class DraftingWorkflowsRepository(detail: WorkflowDetailResponseDto) : WorkflowsRepository {
    private val details = MutableStateFlow<WorkflowDetailResponseDto?>(detail)
    val drafts = MutableStateFlow<List<WorkflowVideoDraft>>(emptyList())
    val answers = MutableStateFlow<Map<String, String>>(emptyMap())

    override fun cards(module: String, date: String, filter: String): Flow<PagingData<WorkflowCardDto>> = flowOf(PagingData.empty())
    override fun observeChips(module: String, date: String): Flow<WorkflowChipsDto?> = flowOf(WorkflowChipsDto())
    override fun observeOverdueDates(module: String): Flow<List<WorkflowOverdueDateDto>> = flowOf(emptyList())
    override fun observeDetail(workflowId: String, lens: String, date: String): Flow<WorkflowDetailResponseDto?> = details
    override fun observeVideoDrafts(workflowId: String): Flow<List<WorkflowVideoDraft>> = drafts
    override suspend fun listVideoDrafts(workflowId: String): List<WorkflowVideoDraft> = drafts.value
    override suspend fun replaceVideoDraft(draft: WorkflowVideoDraft): WorkflowVideoDraft? {
        val previous = drafts.value.firstOrNull { it.fieldKey == draft.fieldKey }
        drafts.value = drafts.value.filterNot { it.fieldKey == draft.fieldKey } + draft
        return previous
    }
    override suspend fun clearVideoDrafts(workflowId: String) { drafts.value = emptyList() }
    override suspend fun markVideoDraftsSubmitting(workflowId: String) {
        drafts.value = drafts.value.map { it.copy(syncStatus = "SUBMITTING") }
    }
    override fun observeStepDraftAnswers(workflowId: String): Flow<Map<String, String>> = answers
    override suspend fun putStepDraftAnswer(workflowId: String, actionId: String, value: String) { answers.value = answers.value + (actionId to value) }
    override suspend fun clearStepDraftAnswers(workflowId: String) { answers.value = emptyMap() }
    override suspend fun refreshDetail(workflowId: String, lens: String, date: String): Result<Unit> = Result.success(Unit)
    override suspend fun findCachedCard(workflowId: String): WorkflowCardDto? = null
    override suspend fun markActionAnswered(workflowId: String, actionId: String, answerValue: String) = Unit
    override suspend fun markActionCompleted(workflowId: String, actionId: String, inReview: Boolean) = Unit
}

private class RecordingWorkflowSync : SyncRepository {
    data class Complete(val actionId: String, val idempotencyKey: String, val proofOutboxItemId: String?, val proofOutboxItems: List<WorkflowProofOutboxRef>)
    data class Answer(val actionId: String, val answerValue: String, val proofOutboxItems: List<WorkflowProofOutboxRef>)

    val completes = mutableListOf<Complete>()
    val answers = mutableListOf<Answer>()
    private val stepItems = mutableListOf<Pair<String, String>>() // (id, opType)
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    fun succeed(count: Int) {
        status.value = SyncStatus.empty(online = true).copy(
            items = stepItems.mapIndexed { i, (id, op) ->
                SyncQueueItem(
                    id = id, idempotencyKey = id, opType = op, groupKey = "wf-death-sop",
                    status = if (i < count) SyncItemStatus.SUCCEEDED else SyncItemStatus.QUEUED,
                    attemptCount = 1, maxAttempts = 5, conflict = false,
                    createdAt = 1_000_000L, updatedAt = 1_000_000L, lastError = null,
                )
            },
        )
    }

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun enqueueWorkflowActionComplete(groupKey: String, idempotencyKey: String, workflowId: String, actionId: String, proofOutboxItemId: String?, proofOutboxItems: List<WorkflowProofOutboxRef>): AppResult<String> {
        completes += Complete(actionId, idempotencyKey, proofOutboxItemId, proofOutboxItems)
        return AppResult.Ok("complete-$actionId").also { stepItems += "complete-$actionId" to "WORKFLOW_ACTION_COMPLETE" }
    }
    override suspend fun enqueueWorkflowActionAnswer(groupKey: String, idempotencyKey: String, workflowId: String, actionId: String, answerValue: String, proofOutboxItemId: String?, proofOutboxItems: List<WorkflowProofOutboxRef>): AppResult<String> {
        answers += Answer(actionId, answerValue, proofOutboxItems)
        return AppResult.Ok("answer-$actionId").also { stepItems += "answer-$actionId" to "WORKFLOW_ACTION_ANSWER" }
    }
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = AppResult.Ok("proof-1")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
