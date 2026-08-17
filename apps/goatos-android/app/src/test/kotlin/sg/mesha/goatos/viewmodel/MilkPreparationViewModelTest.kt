package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
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
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.MilkPreparationRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.MilkPreparationFarmTaskDto
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto
import sg.mesha.goatos.feature.counts.MilkPreparationEvent
import kotlinx.coroutines.flow.first

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class MilkPreparationViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    // A CANCELLED re-capture must leave the existing proof alone. The old order discarded the row
    // first and only then opened the camera, so cancelling it (or a camera failure) deleted a good
    // proof and left the slot empty -- the operator's "proof disappeared".
    @Test
    fun `a cancelled re-capture keeps the existing proof`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        // ONE video available: the first capture consumes it, so the retake finds the camera empty
        // and returns null, which is exactly what a cancel looks like to the ViewModel.
        val videoSource = FakeProofCaptureSource(
            mutableListOf(CapturedVideo(localUri = "/proof/goat-milk-qty.mp4", startedAtMs = 1L, endedAtMs = 2L)),
        )
        val syncRepository = FakeMilkPreparationSyncRepository()
        val draftRepository = FakeMilkPreparationDraftRepository()
        val milkRepository = FakeMilkPreparationRepository()

        val viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = syncRepository,
            repo = milkRepository,
            capture = videoSource,
            proofCaptureRepository = proofCaptureRepository,
            drafts = draftRepository,
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(
                mapOf(
                    MilkPreparationViewModel.ARG_PARK_ID to "park-1",
                ),
            ),
        )
        backgroundScope.launch { viewModel.state.collect {} }

        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "0"))
        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("evening", "0"))
        viewModel.onEvent(MilkPreparationEvent.SetGoatMilkUsed(true))
        viewModel.onEvent(MilkPreparationEvent.SetStepAnswer("goat_milk_quantity", "5"))
        advanceUntilIdle()

        viewModel.onEvent(MilkPreparationEvent.CaptureStep("goat_milk_quantity"))
        advanceUntilIdle()
        assertEquals("the first capture must record one proof", 1, proofCaptureRepository.captureCalls.size)
        assertEquals(
            "milk-prep proofs are not backend-shed-scoped: subject_type must not claim " +
                "'shed' when the id is really the park id -- that poisons subject_type='shed' " +
                "lookups (vaccination/weighing/sop) with a non-shed uuid. No backend task id " +
                "exists at capture time for milk prep, so this is 'other', not 'shed' or 'task'.",
            ProofSubject.OTHER,
            proofCaptureRepository.captureCalls.single().subject,
        )

        // Retake, and cancel it.
        viewModel.onEvent(MilkPreparationEvent.ReCaptureStep("goat_milk_quantity"))
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
        assertEquals("/proof/goat-milk-qty.mp4", survivingRows.first().localUri)
    }

    @Test
    fun `a failed re-capture keeps the existing proof`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        val videoSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "/proof/goat-milk-qty-1.mp4", startedAtMs = 1L, endedAtMs = 2L),
                CapturedVideo(localUri = "/proof/goat-milk-qty-2.mp4", startedAtMs = 3L, endedAtMs = 4L),
            ),
        )
        val syncRepository = FakeMilkPreparationSyncRepository()
        val draftRepository = FakeMilkPreparationDraftRepository()
        val milkRepository = FakeMilkPreparationRepository()

        val viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = syncRepository,
            repo = milkRepository,
            capture = videoSource,
            proofCaptureRepository = proofCaptureRepository,
            drafts = draftRepository,
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(
                mapOf(
                    MilkPreparationViewModel.ARG_PARK_ID to "park-1",
                ),
            ),
        )
        backgroundScope.launch { viewModel.state.collect {} }

        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "0"))
        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("evening", "0"))
        viewModel.onEvent(MilkPreparationEvent.SetGoatMilkUsed(true))
        viewModel.onEvent(MilkPreparationEvent.SetStepAnswer("goat_milk_quantity", "5"))
        advanceUntilIdle()

        viewModel.onEvent(MilkPreparationEvent.CaptureStep("goat_milk_quantity"))
        advanceUntilIdle()
        assertEquals("the first capture must record one proof", 1, proofCaptureRepository.captureCalls.size)

        // Retake, and let it fail.
        proofCaptureRepository.failNextCapture = true
        viewModel.onEvent(MilkPreparationEvent.ReCaptureStep("goat_milk_quantity"))
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
        assertEquals("/proof/goat-milk-qty-1.mp4", survivingRows.first().localUri)
    }

    @Test
    fun `a successful re-capture ends with exactly the new proof`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        val videoSource = FakeProofCaptureSource(
            mutableListOf(
                CapturedVideo(localUri = "/proof/goat-milk-qty-1.mp4", startedAtMs = 1L, endedAtMs = 2L),
                CapturedVideo(localUri = "/proof/goat-milk-qty-2.mp4", startedAtMs = 3L, endedAtMs = 4L),
            ),
        )
        val syncRepository = FakeMilkPreparationSyncRepository()
        val draftRepository = FakeMilkPreparationDraftRepository()
        val milkRepository = FakeMilkPreparationRepository()

        val viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = syncRepository,
            repo = milkRepository,
            capture = videoSource,
            proofCaptureRepository = proofCaptureRepository,
            drafts = draftRepository,
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(
                mapOf(
                    MilkPreparationViewModel.ARG_PARK_ID to "park-1",
                ),
            ),
        )
        backgroundScope.launch { viewModel.state.collect {} }

        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "0"))
        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("evening", "0"))
        viewModel.onEvent(MilkPreparationEvent.SetGoatMilkUsed(true))
        viewModel.onEvent(MilkPreparationEvent.SetStepAnswer("goat_milk_quantity", "5"))
        advanceUntilIdle()

        viewModel.onEvent(MilkPreparationEvent.CaptureStep("goat_milk_quantity"))
        advanceUntilIdle()
        assertEquals("the first capture must record one proof", 1, proofCaptureRepository.captureCalls.size)

        // Retake, and succeed.
        viewModel.onEvent(MilkPreparationEvent.ReCaptureStep("goat_milk_quantity"))
        advanceUntilIdle()

        assertEquals(
            "a successful retake must record two proofs",
            2,
            proofCaptureRepository.captureCalls.size,
        )
        // P1 FIX: Manohar ordering (captureReplacingLatest): new proof is stored first, then the old one
        // is removed from the repository ONCE SYNCED. Only the latest proof survives in Room storage.
        proofCaptureRepository.driveAllPendingRetirements()
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals("only the new proof remains after successful re-capture", 1, survivingRows.size)
        assertEquals("/proof/goat-milk-qty-2.mp4", survivingRows.last().localUri)
        assertEquals(
            "the old proof outbox item must be deleted from sync",
            1,
            syncRepository.deletedOutboxItems.size,
        )
    }

    /** INVARIANT (judge finding #2, 2026-08-16): queued submit survives process death — fresh VM
     *  from persisted SavedStateHandle blocks edits while QUEUED; terminal FAILED unlocks. */
    @Test
    fun `process death with queued submit blocks edits and terminal failed unlocks`() = runTest(dispatcher) {
        fun item(status: sg.mesha.goatos.core.data.sync.SyncItemStatus) =
            sg.mesha.goatos.core.data.sync.SyncQueueItem(
                id = "outbox-prep-1", opType = "MILK_PREPARATION_SUBMIT", idempotencyKey = "k",
                groupKey = "g", status = status, attemptCount = 1, maxAttempts = 8,
                conflict = false, createdAt = 1L, updatedAt = 2L, lastError = null,
            )
        val syncRepository = FakeMilkPreparationSyncRepository()
        syncRepository.itemFlow.value = item(sg.mesha.goatos.core.data.sync.SyncItemStatus.QUEUED)
        val viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = syncRepository,
            repo = FakeMilkPreparationRepository(),
            capture = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            drafts = FakeMilkPreparationDraftRepository(),
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(
                mapOf(
                    MilkPreparationViewModel.ARG_PARK_ID to "park-1",
                    "milkPreparation.submitOutboxItemId" to "outbox-prep-1",
                ),
            ),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "77"))
        advanceUntilIdle()
        org.junit.Assert.assertNotEquals("edits blocked while queued", "77", viewModel.state.value.morningMilkCollected)

        // Terminal failure unlocks (latch cleared) so the operator can fix and resubmit.
        syncRepository.itemFlow.value = item(sg.mesha.goatos.core.data.sync.SyncItemStatus.FAILED).copy(attemptCount = 8)
        advanceUntilIdle()
        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "77"))
        advanceUntilIdle()
        assertEquals("terminal failure must unlock edits", "77", viewModel.state.value.morningMilkCollected)
    }

    @Test
    fun `process death after submit failure unlocks edits (regression #1)`() = runTest(dispatcher) {
        // Regression test for HIGH defect #1: submit failure re-locks after restart
        // Before fix: submitOutboxItemId latch cleared in-memory, but durable draft key persisted
        // On re-entry: durable key restored -> screen stays locked -> operator can't retry
        val syncRepository = FakeMilkPreparationSyncRepository()
        val draftRepository = FakeMilkPreparationDraftRepository()

        // Session 1: submit fails before enqueueing (no outbox item created)
        var viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = syncRepository,
            repo = FakeMilkPreparationRepository(),
            capture = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            drafts = draftRepository,
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(
                mapOf(
                    MilkPreparationViewModel.ARG_PARK_ID to "park-1",
                ),
            ),
        )
        backgroundScope.launch { viewModel.state.collect {} }

        // Set up draft state
        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "10"))
        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("evening", "10"))
        viewModel.onEvent(MilkPreparationEvent.SetGoatMilkUsed(true))
        viewModel.onEvent(MilkPreparationEvent.SetStepAnswer("goat_milk_quantity", "5"))
        advanceUntilIdle()

        // Capture a proof first
        val proofRepository = FakeProofCaptureRepository()
        viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = syncRepository,
            repo = FakeMilkPreparationRepository(),
            capture = FakeProofCaptureSource(
                mutableListOf(CapturedVideo(localUri = "/proof/qty.mp4", startedAtMs = 1L, endedAtMs = 2L)),
            ),
            proofCaptureRepository = proofRepository,
            drafts = draftRepository,
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(
                mapOf(
                    MilkPreparationViewModel.ARG_PARK_ID to "park-1",
                ),
            ),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(MilkPreparationEvent.CaptureStep("goat_milk_quantity"))
        advanceUntilIdle()

        // Submit fails (sync error, not outbox)
        syncRepository.submitResult = AppResult.Err("network error")
        viewModel.onEvent(MilkPreparationEvent.Submit)
        advanceUntilIdle()

        // Before my fix: draft still has submitOutboxItemId set (not cleared)
        // After my fix: both latch and draft are cleared

        // Session 2: process death -> recreate ViewModel
        // Before fix: durable draft still has old submitIdempotencyKey -> screen stays locked
        // After fix: draft was cleared -> screen is editable
        viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = syncRepository,
            repo = FakeMilkPreparationRepository(),
            capture = FakeProofCaptureSource(),
            proofCaptureRepository = proofRepository,
            drafts = draftRepository,
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(
                mapOf(
                    MilkPreparationViewModel.ARG_PARK_ID to "park-1",
                ),
            ),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        // After process death, screen should be editable (submit cleared both latches)
        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "20"))
        advanceUntilIdle()
        assertEquals(
            "after submit failure + process death, screen must be editable so operator can retry",
            "20",
            viewModel.state.value.morningMilkCollected,
        )
    }

    /** Live-status gate (Codex item 4): the backend/Room task already has a submission recorded
     *  (`verification_status = pending_verification`) even though NOTHING was queued on this
     *  phone -- no local submitOutboxItemId latch. A fresh ViewModel must still render read-only:
     *  live server truth (read here via `isEditable`, which derives straight from
     *  `task.verificationStatus`) wins over the remembered local draft state. Unlike
     *  MilkFeedingViewModel, this screen already gated `isEditable`/`morningQuestionEnabled` on
     *  the live `verificationStatus` field before this change -- no production edit was needed
     *  here, only this test to pin the invariant and guard the symmetry with MilkFeeding. */
    @Test
    fun `live pending_verification status blocks edits with no local latch`() = runTest(dispatcher) {
        val viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = FakeMilkPreparationSyncRepository(),
            repo = FakeMilkPreparationRepository(
                seedTask = MilkPreparationFarmTaskDto(parkId = "park-1", verificationStatus = "pending_verification"),
            ),
            capture = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            drafts = FakeMilkPreparationDraftRepository(),
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(mapOf(MilkPreparationViewModel.ARG_PARK_ID to "park-1")),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "77"))
        advanceUntilIdle()
        org.junit.Assert.assertNotEquals(
            "a park the server already recorded as submitted must render read-only even with no local latch",
            "77",
            viewModel.state.value.morningMilkCollected,
        )
    }

    /** The other half of the live-status gate: no server submission recorded (task absent /
     *  `not_submitted`) must leave the screen editable -- paired with the existing
     *  `process death with queued submit blocks edits and terminal failed unlocks` test above,
     *  which already covers the locally-FAILED-latch half of this invariant. */
    @Test
    fun `no server submission leaves screen editable with no local latch`() = runTest(dispatcher) {
        val viewModel = MilkPreparationViewModel(
            feedCompletionStore = FeedCompletionLocalStore(),
            sync = FakeMilkPreparationSyncRepository(),
            repo = FakeMilkPreparationRepository(
                seedTask = MilkPreparationFarmTaskDto(parkId = "park-1", verificationStatus = "not_submitted"),
            ),
            capture = FakeProofCaptureSource(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            drafts = FakeMilkPreparationDraftRepository(),
            analytics = FakeAnalyticsPort(),
            saved = SavedStateHandle(mapOf(MilkPreparationViewModel.ARG_PARK_ID to "park-1")),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(MilkPreparationEvent.SetCollectedMilk("morning", "77"))
        advanceUntilIdle()
        assertEquals(
            "no server submission recorded must leave the screen editable",
            "77",
            viewModel.state.value.morningMilkCollected,
        )
    }

}

private class FakeMilkPreparationSyncRepository : SyncRepository {
    private val status = MutableStateFlow(sg.mesha.goatos.core.data.sync.SyncStatus.empty(online = true))
    val deletedOutboxItems = mutableListOf<String>()
    var submitResult: AppResult<String> = AppResult.Ok("submit-outbox-id")

    override fun observeStatus(): MutableStateFlow<sg.mesha.goatos.core.data.sync.SyncStatus> = status
    val itemFlow = MutableStateFlow<sg.mesha.goatos.core.data.sync.SyncQueueItem?>(null)
    override fun observeItem(itemId: String): Flow<sg.mesha.goatos.core.data.sync.SyncQueueItem?> = itemFlow

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: sg.mesha.goatos.core.network.dto.ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-item-1")

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
    ): AppResult<String> = error("unused")

    override suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueReschedule(
        obligationId: String,
        groupKey: String,
        idempotencyKey: String,
        request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueVerificationVerdict(
        itemId: String,
        decision: String,
        reason: String?,
        rowVersion: Int,
    ): AppResult<String> = error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> {
        deletedOutboxItems.add(itemId)
        return AppResult.Ok(Unit)
    }

    override suspend fun triggerDrain() = Unit

    override suspend fun enqueueMilkPreparationSubmit(
        groupKey: String,
        idempotencyKey: String,
        parkId: String,
        preparationDate: String,
        goatMilkUsed: Boolean,
        answers: sg.mesha.goatos.core.data.sync.MilkPreparationAnswersPayload,
        proofItems: Map<String, String>,
    ): AppResult<String> = submitResult

    override suspend fun enqueueMilkFeedingSubmit(
        groupKey: String,
        idempotencyKey: String,
        taskId: String,
        parkId: String,
        feedingDate: String,
        sessionNo: Int,
        answers: sg.mesha.goatos.core.network.dto.MilkFeedingAnswersDto,
        cleanBottlesProofOutboxItemId: String,
        mixingAndFillingProofOutboxItemId: String,
    ): AppResult<String> = error("unused")
}

private class FakeMilkPreparationDraftRepository : CaptureDraftRepository {
    private val drafts = mutableMapOf<String, CaptureDraft>()

    override suspend fun find(flowKey: String, entityId: String): CaptureDraft {
        return drafts.getOrPut(entityId) { CaptureDraft() }
    }

    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> = MutableStateFlow(drafts[entityId] ?: CaptureDraft())

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) {
        val current = find(flowKey, entityId)
        drafts[entityId] = current.copy(answers = current.answers + answers)
    }

    override suspend fun putProof(flowKey: String, entityId: String, step: String, outboxItemId: String, fingerprint: String?) {
        val current = find(flowKey, entityId)
        drafts[entityId] = current.copy(proofs = current.proofs + (step to outboxItemId))
    }

    override suspend fun putSubmit(flowKey: String, entityId: String, idempotencyKey: String?, outboxItemId: String?) {
        val current = find(flowKey, entityId)
        drafts[entityId] = current.copy(submitIdempotencyKey = idempotencyKey, submitOutboxItemId = outboxItemId)
    }

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) {
        val current = find(flowKey, entityId)
        drafts[entityId] = current.copy(proofs = current.proofs - step)
    }

    override suspend fun clear(flowKey: String, entityId: String) {
        drafts.remove(entityId)
    }

    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> = MutableStateFlow(emptyMap())
}

private class FakeMilkPreparationRepository(
    seedTask: MilkPreparationFarmTaskDto? = null,
) : MilkPreparationRepository {
    private val status = MutableStateFlow(
        sg.mesha.goatos.core.common.Resource<MilkPreparationPageDto>(
            data = seedTask?.let { MilkPreparationPageDto(farmTasks = listOf(it)) },
        ),
    )

    override fun observe(preparationDate: String): Flow<sg.mesha.goatos.core.common.Resource<MilkPreparationPageDto>> = status

    override suspend fun refresh(preparationDate: String): Result<Unit> = Result.success(Unit)
}
