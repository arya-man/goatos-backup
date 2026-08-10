package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
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
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.WorkflowVideoDraft
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowOverdueDateDto
import sg.mesha.goatos.core.network.dto.WorkflowSubjectDto
import sg.mesha.goatos.feature.counts.WorkflowActionSection
import sg.mesha.goatos.feature.counts.WorkflowDeathSubmissionLabel

/**
 * The reported field failure (maintainer, 2026-08-10): on a Death workflow the operator records the
 * FIRST video, and the screen does not move. Progress still reads 0/2, "Record death video" still
 * sits under Scheduled as the live row, and "Record post-mortem video" offers no control at all —
 * so the operator has no next step and no way to reach Submit.
 *
 * The cause is that Death is a TWO-DRAFT flow: neither video is uploaded until Submit, so the
 * backend legitimately keeps both actions `pending` and — because post-mortem is seq 2 in the same
 * section — legitimately returns it `blocked: previous_action`
 * (`tasks/domain.OperatorActionBlocked`). Rendering that backend truth verbatim is what strands the
 * operator. Pre-submit, a durable local draft IS the operator's completed work and must present as
 * such; the backend stays authoritative the moment Submit uploads.
 *
 * These tests drive the real production path (the ViewModel's own detail/draft collection), not the
 * pure helpers, because the defect lived in how the mapper combined them.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WorkflowDeathDraftPresentationTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    /**
     * The exact field case: ONE draft exists, the backend still reports both actions pending with
     * post-mortem blocked behind its predecessor.
     */
    @Test
    fun `first death draft reads as recorded and opens the post-mortem step`() = runTest {
        val repo = FakeWorkflowsRepository(detail = deathDetail())
        val vm = viewModel(repo)
        advanceUntilIdle()

        repo.emitDrafts(listOf(draft(actionId = DEATH_VIDEO_ID)))
        advanceUntilIdle()

        val state = vm.state.first()
        assertEquals("one recorded draft is 1 of 2 for the operator", 1, state.actionsDone)
        assertEquals(2, state.actionsTotal)

        val deathVideo = state.actions.single { it.actionId == DEATH_VIDEO_ID }
        assertTrue("the recorded row carries its draft", deathVideo.hasVideoDraft)
        assertEquals(
            "a recorded row moves out of the live work sections",
            WorkflowActionSection.COMPLETED,
            deathVideo.section,
        )
        assertTrue("the draft stays re-recordable before Submit", deathVideo.canRecordVideo)

        val postMortem = state.actions.single { it.actionId == POST_MORTEM_ID }
        assertTrue("the second video is the operator's next step", postMortem.canRecordVideo)
        assertFalse("the second video has no draft yet", postMortem.hasVideoDraft)

        assertEquals(WorkflowDeathSubmissionLabel.SUBMIT, state.deathSubmissionLabel)
        assertFalse("Submit waits for the second video", state.deathSubmissionEnabled)
    }

    /**
     * The trap in the fix itself: once draft-aware progress reaches 2 of 2, the footer must NOT read
     * "Submitted". Nothing has been uploaded — that is the exact moment Submit has to be tappable.
     */
    @Test
    fun `both drafts enable Submit and never read as already submitted`() = runTest {
        val repo = FakeWorkflowsRepository(detail = deathDetail())
        val vm = viewModel(repo)
        advanceUntilIdle()

        repo.emitDrafts(listOf(draft(DEATH_VIDEO_ID), draft(POST_MORTEM_ID)))
        advanceUntilIdle()

        val state = vm.state.first()
        assertEquals(2, state.actionsDone)
        assertEquals(WorkflowDeathSubmissionLabel.SUBMIT, state.deathSubmissionLabel)
        assertTrue("both drafts recorded — Submit is the next tap", state.deathSubmissionEnabled)
    }

    /** After the upload lands, backend truth is what closes the workflow. */
    @Test
    fun `backend completion is what reads as submitted`() = runTest {
        val repo = FakeWorkflowsRepository(
            detail = deathDetail(deathStatus = "in_review", postMortemStatus = "in_review", postMortemBlocked = false),
        )
        val vm = viewModel(repo)
        advanceUntilIdle()

        val state = vm.state.first()
        assertEquals(2, state.actionsDone)
        assertEquals(WorkflowDeathSubmissionLabel.SUBMITTED, state.deathSubmissionLabel)
        assertFalse(state.deathSubmissionEnabled)
    }

    /**
     * Birth is NOT a draft flow: its videos upload on capture, so a `previous_action` block there is
     * live backend truth and must keep blocking. This pins the death-only scope of the override.
     */
    @Test
    fun `a birth row blocked by its predecessor stays blocked`() = runTest {
        val birth = WorkflowDetailResponseDto(
            workflowId = WORKFLOW_ID,
            module = "birth",
            templateKey = "birth_kid",
            subject = WorkflowSubjectDto(goatId = "goat-1", displayId = "GOAT-1"),
            actions = listOf(
                action(id = "b1", key = "kid_clean", seq = 1, status = "pending"),
                action(id = "b2", key = "iodine_dipping", seq = 2, status = "pending", blocked = true, blockedReason = "previous_action"),
            ),
        )
        val repo = FakeWorkflowsRepository(detail = birth)
        val vm = viewModel(repo)
        advanceUntilIdle()

        val second = vm.state.first().actions.single { it.actionId == "b2" }
        assertFalse("birth uploads on capture — the backend block is live truth", second.canRecordVideo)
    }

    // -----------------------------------------------------------------------

    private fun viewModel(repo: FakeWorkflowsRepository) = WorkflowDetailViewModel(
        repo = repo,
        syncRepository = FakeWorkflowSyncRepository(),
        proofCaptureSource = NoopProofCaptureSource(),
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
        savedStateHandle = SavedStateHandle(mapOf(WorkflowDetailViewModel.ARG_WORKFLOW_ID to WORKFLOW_ID)),
    )

    private fun deathDetail(
        deathStatus: String = "pending",
        postMortemStatus: String = "pending",
        postMortemBlocked: Boolean = true,
    ) = WorkflowDetailResponseDto(
        workflowId = WORKFLOW_ID,
        module = "death",
        templateKey = "death",
        subject = WorkflowSubjectDto(goatId = "goat-1", displayId = "GOAT-1"),
        actions = listOf(
            action(DEATH_VIDEO_ID, "death_video", seq = 1, status = deathStatus),
            action(
                POST_MORTEM_ID,
                "post_mortem_video",
                seq = 2,
                status = postMortemStatus,
                blocked = postMortemBlocked,
                blockedReason = if (postMortemBlocked) "previous_action" else "",
            ),
        ),
    )

    private fun action(
        id: String,
        key: String,
        seq: Int,
        status: String,
        blocked: Boolean = false,
        blockedReason: String = "",
    ) = WorkflowActionDto(
        actionId = id,
        actionKey = key,
        seq = seq,
        section = "main",
        actionType = "action",
        title = key,
        requiresVideo = true,
        status = status,
        blocked = blocked,
        blockedReason = blockedReason,
    )

    private fun draft(actionId: String) = WorkflowVideoDraft(
        id = "draft-$actionId",
        workflowId = WORKFLOW_ID,
        actionId = actionId,
        subjectGoatId = "goat-1",
        localUri = "file:///tmp/$actionId.mp4",
        mimeType = "video/mp4",
        startedAtMs = 1_700_000_000_000,
        endedAtMs = 1_700_000_005_000,
        captureSource = "in_app_camera",
    )

    private companion object {
        const val WORKFLOW_ID = "death-workflow"
        const val DEATH_VIDEO_ID = "action-death-video"
        const val POST_MORTEM_ID = "action-post-mortem"
    }
}

private class FakeWorkflowsRepository(detail: WorkflowDetailResponseDto) : WorkflowsRepository {
    private val details = MutableStateFlow<WorkflowDetailResponseDto?>(detail)
    private val drafts = MutableStateFlow<List<WorkflowVideoDraft>>(emptyList())

    fun emitDrafts(next: List<WorkflowVideoDraft>) {
        drafts.value = next
    }

    override fun cards(module: String, date: String, filter: String): Flow<PagingData<WorkflowCardDto>> =
        flowOf(PagingData.empty())

    override fun observeChips(module: String, date: String): Flow<WorkflowChipsDto?> = flowOf(WorkflowChipsDto())
    override fun observeOverdueDates(module: String): Flow<List<WorkflowOverdueDateDto>> = flowOf(emptyList())
    override fun observeDetail(workflowId: String, lens: String, date: String): Flow<WorkflowDetailResponseDto?> = details
    override fun observeVideoDrafts(workflowId: String): Flow<List<WorkflowVideoDraft>> = drafts
    override suspend fun listVideoDrafts(workflowId: String): List<WorkflowVideoDraft> = drafts.value
    override suspend fun replaceVideoDraft(draft: WorkflowVideoDraft): WorkflowVideoDraft? = null
    override suspend fun clearVideoDrafts(workflowId: String) = Unit
    override suspend fun markVideoDraftsSubmitting(workflowId: String) = Unit
    override suspend fun refreshDetail(workflowId: String, lens: String, date: String): Result<Unit> =
        Result.success(Unit)

    override suspend fun findCachedCard(workflowId: String): WorkflowCardDto? = null
    override suspend fun markActionAnswered(workflowId: String, actionId: String, answerValue: String) = Unit
    override suspend fun markActionCompleted(workflowId: String, actionId: String, inReview: Boolean) = Unit
}

private class FakeWorkflowSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-1")

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

private class NoopProofCaptureSource : ProofCaptureSource {
    override suspend fun captureVideo(captureContext: ProofCaptureContext?): CapturedVideo? = null
    override suspend fun pickVideo(): CapturedVideo? = null
}
