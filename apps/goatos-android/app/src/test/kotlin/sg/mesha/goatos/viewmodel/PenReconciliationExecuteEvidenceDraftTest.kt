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
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.PenReconciliationMeta
import sg.mesha.goatos.core.data.PenReconciliationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCardDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.counts.PenReconciliationExecuteEvent

/**
 * The Reconcile execute screen's evidence contract (docs/decisions/pen-reconciliation.md):
 *  - "Mark done" REFUSES to submit without the mandatory pen return video;
 *  - the completion couples to the recorded video's PROOF_UPLOAD outbox id;
 *  - the recorded clip and the completion's stable idempotency key survive Back + re-entry (a NEW
 *    ViewModel with a FRESH SavedStateHandle), so a resend collapses onto the original submission.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PenReconciliationExecuteEvidenceDraftTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `mark done refuses without the mandatory video`() = runTest(dispatcher) {
        val sync = FakePenReconciliationSyncRepository()
        val vm = newViewModel(FakePenReconciliationCardRepository(), PenReconciliationDraftFake(), sync)
        advanceUntilIdle()

        assertFalse("no video, not submittable", vm.state.value.canComplete)
        vm.onEvent(PenReconciliationExecuteEvent.MarkDone)
        advanceUntilIdle()

        assertTrue("nothing may be enqueued without the video", sync.completeKeys.isEmpty())
    }

    @Test
    fun `mark done couples the completion to the recorded video's outbox item`() = runTest(dispatcher) {
        val sync = FakePenReconciliationSyncRepository()
        val drafts = PenReconciliationDraftFake()
        val vm = newViewModel(FakePenReconciliationCardRepository(), drafts, sync)
        advanceUntilIdle()

        vm.onEvent(PenReconciliationExecuteEvent.RecordVideo)
        advanceUntilIdle()
        assertTrue("the capture should mark the video recorded", vm.state.value.videoCaptured)
        assertTrue("one video is all a return needs", vm.state.value.canComplete)

        vm.onEvent(PenReconciliationExecuteEvent.MarkDone)
        advanceUntilIdle()

        assertEquals(listOf(CARD_ID), sync.completeGroups)
        assertEquals(
            "the completion must name the recorded video's PROOF_UPLOAD outbox id",
            listOf(drafts.rows[CARD_ID]?.proofs?.get("return")),
            sync.completeProofItemIds,
        )
    }

    @Test
    fun `a recorded video survives leaving and re-entering the card`() = runTest(dispatcher) {
        val repo = FakePenReconciliationCardRepository()
        val drafts = PenReconciliationDraftFake()
        val sync = FakePenReconciliationSyncRepository()
        val proofRepo = FakeProofCaptureRepository()

        val first = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()
        first.onEvent(PenReconciliationExecuteEvent.RecordVideo)
        advanceUntilIdle()
        assertTrue(first.state.value.videoCaptured)
        assertEquals(1, proofRepo.captureCalls.size)

        // Back: the destination is popped, so its SavedStateHandle is gone. Re-entry = new VM.
        val reentered = newViewModel(repo, drafts, sync, proofRepo)
        advanceUntilIdle()

        assertTrue("re-entering must keep the recorded video", reentered.state.value.videoCaptured)
        assertTrue("and must stay submittable", reentered.state.value.canComplete)
        assertEquals("re-entry must not ask for a second recording", 1, proofRepo.captureCalls.size)
    }

    @Test
    fun `completion after re-entry reuses the card's idempotency key`() = runTest(dispatcher) {
        val repo = FakePenReconciliationCardRepository()
        val drafts = PenReconciliationDraftFake()
        val sync = FakePenReconciliationSyncRepository()

        val first = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        first.onEvent(PenReconciliationExecuteEvent.RecordVideo)
        advanceUntilIdle()
        first.onEvent(PenReconciliationExecuteEvent.MarkDone)
        advanceUntilIdle()
        val firstKey = sync.completeKeys.single()

        val reentered = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        reentered.onEvent(PenReconciliationExecuteEvent.MarkDone)
        advanceUntilIdle()

        assertEquals(
            "every completion for one card must carry the same key",
            listOf(firstKey, firstKey),
            sync.completeKeys,
        )
    }

    @Test
    fun `an accepted completion clears the draft and returns to the list`() = runTest(dispatcher) {
        val repo = FakePenReconciliationCardRepository()
        val drafts = PenReconciliationDraftFake()
        val sync = FakePenReconciliationSyncRepository()

        val vm = newViewModel(repo, drafts, sync)
        advanceUntilIdle()
        vm.onEvent(PenReconciliationExecuteEvent.RecordVideo)
        advanceUntilIdle()
        vm.onEvent(PenReconciliationExecuteEvent.MarkDone)
        advanceUntilIdle()
        assertEquals(1, drafts.rows.size)

        sync.succeed(sync.completeItemIds.last())
        advanceUntilIdle()

        assertNull("a finished card keeps no draft", drafts.rows[CARD_ID])
        assertTrue("the host is told to pop back to the list", vm.state.value.returnToList)
        assertEquals(listOf(CARD_ID), repo.forgotten)
    }

    private fun newViewModel(
        repo: FakePenReconciliationCardRepository,
        drafts: PenReconciliationDraftFake,
        sync: FakePenReconciliationSyncRepository,
        proofCaptureRepository: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        analytics: AnalyticsPort = FakeAnalyticsPort(),
    ) = PenReconciliationExecuteViewModel(
        repo = repo,
        drafts = drafts,
        syncRepository = sync,
        proofCaptureSource = AlwaysCapturingPenReturnSource(),
        proofCaptureRepository = proofCaptureRepository,
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        // A FRESH handle every time: this is what "Back then re-open" does to the destination.
        savedStateHandle = SavedStateHandle(mapOf("card_id" to CARD_ID)),
    )

    private companion object {
        const val CARD_ID = "11111111-1111-4111-8111-111111111111"
    }
}

private const val REGISTERED_SHED_ID = "22222222-2222-4222-8222-222222222222"

/** In-memory stand-in for the Room-backed Reconcile cache. */
private class FakePenReconciliationCardRepository : PenReconciliationRepository {
    val forgotten = mutableListOf<String>()
    override val meta: StateFlow<PenReconciliationMeta> = MutableStateFlow(PenReconciliationMeta())

    override fun cards(status: String): Flow<PagingData<CountsPenReconciliationCardDto>> =
        flowOf(PagingData.empty())

    override suspend fun forgetCompleted(cardId: String) {
        forgotten += cardId
    }

    override suspend fun findCached(cardId: String) = CountsPenReconciliationCardDto(
        cardId = cardId,
        status = "open",
        primaryActionKey = "execute",
        goatId = "goat-1",
        goatDisplayId = "G-77",
        scannedIdentifier = "982000123456789",
        foundOperationalLocationDisplay = "Castro 2",
        registeredShedId = REGISTERED_SHED_ID,
        registeredShedName = "Castro",
        registeredOperationalLocationDisplay = "Castro 1",
    )
}

/** In-memory stand-in for the Room-backed shared capture-draft store. */
private class PenReconciliationDraftFake : CaptureDraftRepository {
    val rows = mutableMapOf<String, CaptureDraft>()
    private val progress = MutableStateFlow<Map<String, Int>>(emptyMap())

    override suspend fun find(flowKey: String, entityId: String): CaptureDraft =
        rows[entityId] ?: CaptureDraft()

    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> =
        MutableStateFlow(rows[entityId] ?: CaptureDraft())

    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> = progress

    override suspend fun putProof(
        flowKey: String,
        entityId: String,
        step: String,
        outboxItemId: String,
        fingerprint: String?,
    ) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(proofs = current.proofs + (step to outboxItemId))
    }

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) {
        val current = rows[entityId] ?: return
        rows[entityId] = current.copy(proofs = current.proofs - step)
    }

    override suspend fun putSubmit(
        flowKey: String,
        entityId: String,
        idempotencyKey: String?,
        outboxItemId: String?,
    ) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(submitIdempotencyKey = idempotencyKey, submitOutboxItemId = outboxItemId)
    }

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) = Unit

    override suspend fun clear(flowKey: String, entityId: String) {
        rows.remove(entityId)
    }
}

private class FakePenReconciliationSyncRepository : SyncRepository {
    var proofEnqueues: Int = 0
        private set
    val completeKeys = mutableListOf<String>()
    val completeGroups = mutableListOf<String>()
    val completeProofItemIds = mutableListOf<String>()
    val completeItemIds = mutableListOf<String>()
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    fun succeed(itemId: String) {
        val now = 1_700_000_000_000
        status.value = SyncStatus(
            online = true,
            pendingCount = 0,
            inFlightCount = 0,
            failedCount = 0,
            deadLetterCount = 0,
            lastSyncAt = now,
            items = listOf(
                SyncQueueItem(
                    id = itemId,
                    idempotencyKey = "test-idempotency-key",
                    opType = "PEN_RECONCILIATION_COMPLETE",
                    groupKey = "group",
                    status = SyncItemStatus.SUCCEEDED,
                    attemptCount = 1,
                    maxAttempts = 5,
                    conflict = false,
                    createdAt = now,
                    updatedAt = now,
                    lastError = null,
                ),
            ),
        )
    }

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> {
        proofEnqueues += 1
        return AppResult.Ok("proof-$proofEnqueues")
    }

    override suspend fun enqueuePenReconciliationComplete(
        groupKey: String,
        idempotencyKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> {
        completeKeys += idempotencyKey
        completeGroups += groupKey
        completeProofItemIds += proofOutboxItemId
        val id = "complete-${completeKeys.size}"
        completeItemIds += id
        return AppResult.Ok(id)
    }

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

private class AlwaysCapturingPenReturnSource : ProofCaptureSource {
    private var count = 0
    override suspend fun captureVideo(captureContext: ProofCaptureContext?): CapturedVideo {
        count += 1
        return CapturedVideo(
            localUri = "file:///tmp/clip-$count.mp4",
            mimeType = "video/mp4",
            captureSource = "in_app_camera",
            startedAtMs = 1_700_000_000_000,
            endedAtMs = 1_700_000_005_000,
        )
    }

    override suspend fun pickVideo(): CapturedVideo = captureVideo()
}
