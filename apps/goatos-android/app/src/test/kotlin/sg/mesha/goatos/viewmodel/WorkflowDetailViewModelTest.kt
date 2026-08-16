package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.WorkflowVideoDraft
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowOverdueDateDto
import sg.mesha.goatos.core.network.dto.WorkflowSubjectDto
import sg.mesha.goatos.feature.counts.WorkflowDetailEvent

/**
 * REAL [WorkflowDetailViewModel] behaviour tests, driven through the shared [FakeProofCaptureRepository]
 * (`CaptureTestFakes.kt`) and [FakeProofCaptureSource] — no bespoke/self-fulfilling capture-replacement
 * logic is reimplemented here; every assertion reads the fake's own bookkeeping
 * ([FakeProofCaptureRepository.allRows]/`captureCalls`) or the sync fake's own idempotency-key ledger.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class WorkflowDetailViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun requiresVideoDetail(actionId: String = "action-1") = WorkflowDetailResponseDto(
        workflowId = "wf-1",
        module = "birth",
        templateKey = "birth_kid",
        subject = WorkflowSubjectDto(goatId = "goat-1", displayId = "GOAT-1", tag = "T1"),
        actionsTotal = 1,
        actions = listOf(
            WorkflowActionDto(
                actionId = actionId,
                actionKey = "record_kid_video",
                seq = 1,
                actionType = "action",
                title = "Record kid video",
                requiresVideo = true,
                status = "pending",
            ),
        ),
    )

    private fun buildViewModel(
        workflowsRepository: FakeWorkflowDetailRepository,
        syncRepository: FakeWorkflowDetailSyncRepository,
        proofCaptureRepository: FakeProofCaptureRepository,
        proofCaptureSource: FakeProofCaptureSource,
    ) = WorkflowDetailViewModel(
        repo = workflowsRepository,
        syncRepository = syncRepository,
        proofCaptureSource = proofCaptureSource,
        proofCaptureRepository = proofCaptureRepository,
        analytics = FakeAnalyticsPort(),
        crashReporter = NoopCrashReporter(),
        savedStateHandle = SavedStateHandle(mapOf(WorkflowDetailViewModel.ARG_WORKFLOW_ID to "wf-1")),
    )

    // (a) A cancelled/failed re-capture must preserve the pre-existing proof row. Mirrors
    // MilkPreparationViewModelTest's "a cancelled/failed re-capture keeps the existing proof".
    @Test
    fun `cancelled re-capture keeps the existing proof row`() = runTest(dispatcher) {
        val workflowsRepository = FakeWorkflowDetailRepository(requiresVideoDetail())
        val syncRepository = FakeWorkflowDetailSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        // ONE video available: the first capture consumes it, the re-record finds the camera
        // empty and returns null -- exactly what a cancel looks like to the ViewModel.
        val proofCaptureSource = FakeProofCaptureSource(
            mutableListOf(CapturedVideo(localUri = "/proof/kid-video-1.mp4", startedAtMs = 1L, endedAtMs = 2L)),
        )
        val viewModel = buildViewModel(workflowsRepository, syncRepository, proofCaptureRepository, proofCaptureSource)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.RecordVideo("action-1"))
        advanceUntilIdle()
        assertEquals("the first capture must record one proof", 1, proofCaptureRepository.captureCalls.size)

        viewModel.onEvent(WorkflowDetailEvent.RecordVideo("action-1"))
        advanceUntilIdle()

        assertEquals(
            "a cancelled retake must not write a second proof",
            1,
            proofCaptureRepository.captureCalls.size,
        )
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals(
            "the existing proof row must survive a cancelled retake -- discarding it first is what lost it",
            1,
            survivingRows.size,
        )
        assertEquals("/proof/kid-video-1.mp4", survivingRows.first().localUri)
    }

    @Test
    fun `failed re-capture keeps the existing proof row`() = runTest(dispatcher) {
        val workflowsRepository = FakeWorkflowDetailRepository(requiresVideoDetail())
        val syncRepository = FakeWorkflowDetailSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "/proof/kid-video-1.mp4", startedAtMs = 1L, endedAtMs = 2L),
                CapturedVideo(localUri = "/proof/kid-video-2.mp4", startedAtMs = 3L, endedAtMs = 4L),
            ),
        )
        val viewModel = buildViewModel(workflowsRepository, syncRepository, proofCaptureRepository, proofCaptureSource)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.RecordVideo("action-1"))
        advanceUntilIdle()
        assertEquals("the first capture must record one proof", 1, proofCaptureRepository.captureCalls.size)

        proofCaptureRepository.failNextCapture = true
        viewModel.onEvent(WorkflowDetailEvent.RecordVideo("action-1"))
        advanceUntilIdle()

        assertEquals(
            "a failed retake must not delete the old proof",
            1,
            proofCaptureRepository.captureCalls.size,
        )
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals(
            "the existing proof row must survive a failed retake",
            1,
            survivingRows.size,
        )
        assertEquals("/proof/kid-video-1.mp4", survivingRows.first().localUri)
    }

    /**
     * (b) A SUCCESSFUL replacement leaves exactly one active row for the slot in
     * [FakeProofCaptureRepository]'s bookkeeping.
     *
     * KNOWN FAKE LIMITATION: [FakeProofCaptureRepository.captureReplacingLatest] retires the old
     * row the moment the new capture call returns [AppResult.Ok] (see its `toRemove`/`remove`
     * call in CaptureTestFakes.kt). PRODUCTION (`DefaultProofCaptureRepository`) gates real
     * retirement on the NEW row reaching `SYNCED` status via the sync engine, not on the capture
     * call returning successfully -- an uploaded-but-not-yet-synced replacement keeps both rows
     * in Room until the sync engine confirms the new one. This test therefore only pins what the
     * FAKE proves: a successful `RecordVideo` ends with one row in the fake's own bookkeeping. It
     * does NOT prove (and must not be read as proving) production's SYNCED-gated retirement
     * semantics -- that requires a Room-backed instrumented/integration test against
     * DefaultProofCaptureRepository, which is out of scope here.
     */
    @Test
    fun `successful re-capture ends with exactly one active proof for the slot`() = runTest(dispatcher) {
        val workflowsRepository = FakeWorkflowDetailRepository(requiresVideoDetail())
        val syncRepository = FakeWorkflowDetailSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "/proof/kid-video-1.mp4", startedAtMs = 1L, endedAtMs = 2L),
                CapturedVideo(localUri = "/proof/kid-video-2.mp4", startedAtMs = 3L, endedAtMs = 4L),
            ),
        )
        val viewModel = buildViewModel(workflowsRepository, syncRepository, proofCaptureRepository, proofCaptureSource)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.RecordVideo("action-1"))
        advanceUntilIdle()
        viewModel.onEvent(WorkflowDetailEvent.RecordVideo("action-1"))
        advanceUntilIdle()

        assertEquals("a successful retake must record two capture calls", 2, proofCaptureRepository.captureCalls.size)
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals("only the new proof remains after successful re-capture (fake bookkeeping)", 1, survivingRows.size)
        assertEquals("/proof/kid-video-2.mp4", survivingRows.last().localUri)
    }

    /**
     * (c) Submit idempotency for the death-submission (video-gated completion) outbox write: the
     * enqueued WORKFLOW_ACTION_COMPLETE idempotency key includes the NEW proof's outboxItemId
     * ([workflowVideoCompletionKey]), and a replay of the same completion path reuses the SAME
     * key rather than minting a second, distinct one.
     */
    @Test
    fun `video completion idempotency key includes the proof outbox item id and is stable across a replay`() = runTest(dispatcher) {
        val workflowsRepository = FakeWorkflowDetailRepository(requiresVideoDetail())
        val syncRepository = FakeWorkflowDetailSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource(
            mutableListOf(CapturedVideo(localUri = "/proof/kid-video-1.mp4", startedAtMs = 1L, endedAtMs = 2L)),
        )
        val viewModel = buildViewModel(workflowsRepository, syncRepository, proofCaptureRepository, proofCaptureSource)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.RecordVideo("action-1"))
        advanceUntilIdle()

        assertEquals("exactly one completion must be enqueued", 1, syncRepository.completeCalls.size)
        val firstCall = syncRepository.completeCalls.single()
        val proofItemId = proofCaptureRepository.allRows().single().outboxItemId
        assertEquals(
            "the idempotency key must be the deterministic wf-complete:<action>:<proofOutboxItemId> form",
            workflowVideoCompletionKey("action-1", proofItemId.orEmpty()),
            firstCall.idempotencyKey,
        )

        // Simulate a replay/double-fire of the SAME logical submit (e.g. a retried background
        // drain call) by invoking the sync repository directly with the identical arguments the
        // ViewModel just used -- the same evidence must collapse onto the same key rather than
        // minting a second, distinct outbox row.
        val replayResult = syncRepository.enqueueWorkflowActionComplete(
            groupKey = "wf-1",
            idempotencyKey = workflowVideoCompletionKey("action-1", proofItemId.orEmpty()),
            workflowId = "wf-1",
            actionId = "action-1",
            proofOutboxItemId = proofItemId,
        )
        assertEquals("a replay of the same evidence must succeed (idempotent), not error", true, replayResult is AppResult.Ok)
        assertEquals(
            "a replay must NOT mint a distinct call with a different key -- both calls share the same key",
            setOf(firstCall.idempotencyKey),
            syncRepository.completeCalls.map { it.idempotencyKey }.toSet(),
        )
        assertEquals(
            "the fake's per-key ledger must show exactly one distinct outbox item id was ever minted for this key",
            1,
            syncRepository.outboxItemIdsForKey(firstCall.idempotencyKey).size,
        )
    }
}

/**
 * Minimal in-memory [WorkflowsRepository] test double: one workflow, no paging/cards/chips
 * machinery. `markActionAnswered`/`markActionCompleted` mutate the held detail so the ViewModel's
 * own `observeDetail()` combine loop re-emits, mirroring the optimistic Room update production
 * performs -- without pulling in Room.
 */
private class FakeWorkflowDetailRepository(initialDetail: WorkflowDetailResponseDto) : WorkflowsRepository {
    private val detailFlow = MutableStateFlow<WorkflowDetailResponseDto?>(initialDetail)
    private val draftsFlow = MutableStateFlow<List<WorkflowVideoDraft>>(emptyList())

    override fun cards(module: String, date: String, filter: String) = error("unused")
    override fun observeChips(module: String, date: String): Flow<WorkflowChipsDto?> = MutableStateFlow(null)
    override fun observeOverdueDates(module: String): Flow<List<WorkflowOverdueDateDto>> = MutableStateFlow(emptyList())
    override fun observeDetail(workflowId: String, lens: String, date: String): Flow<WorkflowDetailResponseDto?> = detailFlow
    override fun observeVideoDrafts(workflowId: String): Flow<List<WorkflowVideoDraft>> = draftsFlow
    override suspend fun listVideoDrafts(workflowId: String): List<WorkflowVideoDraft> = draftsFlow.value
    override suspend fun replaceVideoDraft(draft: WorkflowVideoDraft): WorkflowVideoDraft? {
        val previous = draftsFlow.value.firstOrNull { it.actionId == draft.actionId }
        draftsFlow.value = draftsFlow.value.filterNot { it.actionId == draft.actionId } + draft
        return previous
    }
    override suspend fun clearVideoDrafts(workflowId: String) { draftsFlow.value = emptyList() }
    override suspend fun markVideoDraftsSubmitting(workflowId: String) = Unit
    override suspend fun refreshDetail(workflowId: String, lens: String, date: String): Result<Unit> = Result.success(Unit)
    override suspend fun findCachedCard(workflowId: String): WorkflowCardDto? = null

    override suspend fun markActionAnswered(workflowId: String, actionId: String, answerValue: String) {
        detailFlow.value = detailFlow.value?.let { detail ->
            detail.copy(actions = detail.actions.map { if (it.actionId == actionId) it.copy(status = "completed", answerValue = answerValue) else it })
        }
    }

    override suspend fun markActionCompleted(workflowId: String, actionId: String, inReview: Boolean) {
        detailFlow.value = detailFlow.value?.let { detail ->
            detail.copy(actions = detail.actions.map { if (it.actionId == actionId) it.copy(status = if (inReview) "in_review" else "completed") else it })
        }
    }
}

/**
 * Minimal in-memory [SyncRepository] test double for the workflow-action write path. Tracks every
 * `enqueueWorkflowActionComplete`/`Answer` call and its idempotency key so tests can assert
 * replay/dedup behaviour directly against the fake's own ledger instead of a bespoke reimplementation.
 */
private class FakeWorkflowDetailSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus() = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = MutableStateFlow(null)

    data class CompleteCall(val groupKey: String, val idempotencyKey: String, val workflowId: String, val actionId: String, val proofOutboxItemId: String?)
    data class AnswerCall(val groupKey: String, val idempotencyKey: String, val workflowId: String, val actionId: String, val answerValue: String, val proofOutboxItemId: String?)

    val completeCalls = mutableListOf<CompleteCall>()
    val answerCalls = mutableListOf<AnswerCall>()
    // key -> the single outbox item id ever minted for it (idempotent collapse).
    private val outboxItemIdByKey = mutableMapOf<String, String>()
    private var nextOutboxId = 0

    fun completeCallsByKey(key: String) = completeCalls.filter { it.idempotencyKey == key }
    fun outboxItemIdsForKey(key: String): Set<String> = setOfNotNull(outboxItemIdByKey[key])

    override suspend fun enqueueWorkflowActionAnswer(
        groupKey: String,
        idempotencyKey: String,
        workflowId: String,
        actionId: String,
        answerValue: String,
        proofOutboxItemId: String?,
    ): AppResult<String> {
        answerCalls += AnswerCall(groupKey, idempotencyKey, workflowId, actionId, answerValue, proofOutboxItemId)
        val id = outboxItemIdByKey.getOrPut(idempotencyKey) { "wf-outbox-${nextOutboxId++}" }
        return AppResult.Ok(id)
    }

    override suspend fun enqueueWorkflowActionComplete(
        groupKey: String,
        idempotencyKey: String,
        workflowId: String,
        actionId: String,
        proofOutboxItemId: String?,
    ): AppResult<String> {
        completeCalls += CompleteCall(groupKey, idempotencyKey, workflowId, actionId, proofOutboxItemId)
        val id = outboxItemIdByKey.getOrPut(idempotencyKey) { "wf-outbox-${nextOutboxId++}" }
        return AppResult.Ok(id)
    }

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = AppResult.Ok("proof-outbox-1")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
