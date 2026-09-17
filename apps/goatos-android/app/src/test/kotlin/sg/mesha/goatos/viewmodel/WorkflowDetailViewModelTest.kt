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
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.data.sync.WorkflowProofOutboxRef
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowOverdueDateDto
import sg.mesha.goatos.core.network.dto.WorkflowSubjectDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
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
        photoCaptureSource: sg.mesha.goatos.capture.PhotoCaptureSource = NoopPhotoCaptureSource(),
    ) = WorkflowDetailViewModel(
        repo = workflowsRepository,
        syncRepository = syncRepository,
        proofCaptureSource = proofCaptureSource,
        proofCaptureRepository = proofCaptureRepository,
        analytics = FakeAnalyticsPort(),
        crashReporter = NoopCrashReporter(),
        savedStateHandle = SavedStateHandle(mapOf(WorkflowDetailViewModel.ARG_WORKFLOW_ID to "wf-1")),
        photoCaptureSource = photoCaptureSource,
        countsRepository = FakeAddCountsRepository(),
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
     * P1 FIX (CRITICAL): [FakeProofCaptureRepository.captureReplacingLatest] now defers old-row
     * retirement until the new row reaches SYNCED, matching production behavior. Tests can call
     * [FakeProofCaptureRepository.driveAllPendingRetirements] to simulate the new row reaching
     * SYNCED and complete the replacement. This ensures tests can catch regressions where a failed
     * new upload would have destroyed the old evidence — the old row survives until the new one
     * is durably stored server-side.
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
        // P1 FIX: drive pending retirement to complete the replacement.
        proofCaptureRepository.driveAllPendingRetirements()
        val survivingRows = proofCaptureRepository.allRows()
        assertEquals("only the new proof remains after successful re-capture", 1, survivingRows.size)
        assertEquals("/proof/kid-video-2.mp4", survivingRows.last().localUri)
    }

    // A death card is about a real tagged animal the operator finds by its physical RFID, so the
    // header must lead with the tag; the G-… passport id is only a fallback when no tag exists.
    @Test
    fun `death detail headlines the RFID tag and falls back to the passport id only when no tag exists`() = runTest(dispatcher) {
        val deathDetail = requiresVideoDetail().copy(
            module = "death",
            templateKey = "death",
            subject = WorkflowSubjectDto(goatId = "goat-1", displayId = "G-000123", tag = "982000123456789"),
        )
        val workflowsRepository = FakeWorkflowDetailRepository(deathDetail)
        val viewModel = buildViewModel(
            workflowsRepository,
            FakeWorkflowDetailSyncRepository(),
            FakeProofCaptureRepository(),
            FakeProofCaptureSource(mutableListOf()),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()
        assertEquals("982000123456789", viewModel.state.value.displayId)

        // No tag on the animal -> the passport id is the honest fallback, never a blank header.
        val untagged = deathDetail.copy(subject = deathDetail.subject.copy(tag = ""))
        val untaggedRepository = FakeWorkflowDetailRepository(untagged)
        val fallbackViewModel = buildViewModel(
            untaggedRepository,
            FakeWorkflowDetailSyncRepository(),
            FakeProofCaptureRepository(),
            FakeProofCaptureSource(mutableListOf()),
        )
        backgroundScope.launch { fallbackViewModel.state.collect {} }
        advanceUntilIdle()
        assertEquals("G-000123", fallbackViewModel.state.value.displayId)
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

    @Test
    fun `rework recovery submits fresh corrected proofs and ignores old rejected proof refs`() = runTest(dispatcher) {
        val action = WorkflowActionDto(
            actionId = "action-1",
            actionKey = "record_kid_video",
            seq = 1,
            actionType = "action",
            title = "Record kid video",
            status = "rework",
            proofMinVideos = 2,
            // A verifier reject clears the step's proofs on the server (tasks/domain.ApplyStepVerdict).
            proofRefs = emptyList(),
        )
        val workflowsRepository = FakeWorkflowDetailRepository(requiresVideoDetail().copy(actions = listOf(action)))
        val syncRepository = FakeWorkflowDetailSyncRepository()
        // The rejected round's step write is still on this phone, queued after its capture.
        syncRepository.holdOutboxItems(heldStepWrite("wf-complete:action-1:old", createdAt = 1L, opType = "WORKFLOW_ACTION_COMPLETE"))
        val proofCaptureRepository = FakeProofCaptureRepository()
        proofCaptureRepository.seedProofs(
            workflowProofRow(
                id = "old-row",
                fieldKey = workflowProofFieldKey("action-1") + "_video_1",
                serverProofId = "old-proof",
                outboxItemId = "old-outbox",
                capturedAtMs = 1L,
            ),
            workflowProofRow(
                id = "fresh-row-1",
                fieldKey = workflowProofFieldKey("action-1") + "_video_1",
                serverProofId = "new-proof-1",
                outboxItemId = "fresh-outbox-1",
                capturedAtMs = 2L,
            ),
            workflowProofRow(
                id = "fresh-row-2",
                fieldKey = workflowProofFieldKey("action-1") + "_video_2",
                serverProofId = null,
                outboxItemId = "fresh-outbox-2",
                capturedAtMs = 3L,
            ),
        )
        val viewModel = buildViewModel(
            workflowsRepository,
            syncRepository,
            proofCaptureRepository,
            FakeProofCaptureSource(mutableListOf()),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals("fresh corrected rework proofs should complete after process recreation", 1, syncRepository.completeCalls.size)
        assertEquals(
            listOf("new-proof-1", ""),
            syncRepository.completeCalls.single().proofOutboxItems.map { it.proofRef },
        )
        assertEquals(
            listOf("fresh-outbox-1", "fresh-outbox-2"),
            syncRepository.completeCalls.single().proofOutboxItems.map { it.outboxItemId },
        )
    }

    /** Realme E2E 2026-09-17: a PHOTO step said "Video saved on this phone…". */
    @Test
    fun `a photo step reports a saved photo, never a saved video`() = runTest(dispatcher) {
        val action = WorkflowActionDto(
            actionId = "action-1",
            actionKey = "kid_photo",
            seq = 1,
            actionType = "action",
            title = "Kid photo",
            status = "pending",
            proofMinPhotos = 1,
        )
        val workflowsRepository = FakeWorkflowDetailRepository(requiresVideoDetail().copy(actions = listOf(action)))
        val syncRepository = FakeWorkflowDetailSyncRepository()
        val photos = object : sg.mesha.goatos.capture.PhotoCaptureSource {
            override suspend fun capturePhoto(context: sg.mesha.goatos.capture.PhotoCaptureContext) =
                sg.mesha.goatos.capture.CapturedPhoto(localUri = "file:///kid.jpg", capturedAtMs = 5L)
        }
        val viewModel = buildViewModel(
            workflowsRepository,
            syncRepository,
            FakeProofCaptureRepository(),
            FakeProofCaptureSource(mutableListOf()),
            photos,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.TakePhoto("action-1"))
        advanceUntilIdle()

        assertEquals(1, syncRepository.completeCalls.size)
        assertEquals(sg.mesha.goatos.feature.counts.WorkflowProofSavedKind.PHOTO, viewModel.state.value.proofSaved)
        assertEquals("the photo banner replaces the old video sentence", null, viewModel.state.value.message)
    }

    /**
     * Realme E2E 2026-09-17 (P0): ONE photo on a photo-only step enqueued TWO completions -- the
     * durable-capture recovery picked the new row up while the capture was still returning, and the
     * capture then appended the SAME outbox item again, so the second op carried it twice under a
     * different key. The server refused it 409 action_already_completed and the dead row held the
     * workflow's lane. One capture must give one proof entry and one write.
     */
    @Test
    fun `one photo on a photo step enqueues exactly one completion carrying it once`() = runTest(dispatcher) {
        val action = WorkflowActionDto(
            actionId = "action-1",
            actionKey = "pen_gate_photo",
            seq = 1,
            actionType = "action",
            taskType = "photo_record",
            title = "Photo of the pen gate",
            status = "pending",
            proofMinPhotos = 1,
        )
        val workflowsRepository = FakeWorkflowDetailRepository(requiresVideoDetail().copy(actions = listOf(action)))
        val syncRepository = FakeWorkflowDetailSyncRepository().apply { suspendOnEnqueue = true }
        val fakeProofs = FakeProofCaptureRepository()
        // Production's capture returns only after its Room + outbox writes, so the row (already
        // carrying its upload outbox id) is observable BEFORE the capture call hands it back.
        val proofs = object : ProofCaptureRepository by fakeProofs {
            override suspend fun captureReplacingLatest(
                slot: sg.mesha.goatos.core.data.capture.EvidenceSlot,
                subject: ProofSubject,
                subjectId: String?,
                localUri: String,
                mimeType: String,
                caption: String?,
                rfidTag: String?,
                scopeType: String,
                scopeId: String,
                capturedStartMs: Long,
                capturedEndMs: Long,
                capturedByPrincipalId: String?,
                proofPolicy: sg.mesha.goatos.core.data.forms.ProofPolicy,
                awaitUploadEnqueue: Boolean,
                uploadGroupKey: String?,
            ): AppResult<ProofCaptureRow> {
                val result = fakeProofs.captureReplacingLatest(
                    slot, subject, subjectId, localUri, mimeType, caption, rfidTag, scopeType, scopeId,
                    capturedStartMs, capturedEndMs, capturedByPrincipalId, proofPolicy, awaitUploadEnqueue, uploadGroupKey,
                )
                repeat(3) { kotlinx.coroutines.yield() }
                return result
            }
        }
        val photos = object : sg.mesha.goatos.capture.PhotoCaptureSource {
            override suspend fun capturePhoto(context: sg.mesha.goatos.capture.PhotoCaptureContext) =
                sg.mesha.goatos.capture.CapturedPhoto(localUri = "file:///gate.jpg", capturedAtMs = 5L)
        }
        val analytics = FakeAnalyticsPort()
        val viewModel = WorkflowDetailViewModel(
            repo = workflowsRepository,
            syncRepository = syncRepository,
            proofCaptureSource = FakeProofCaptureSource(mutableListOf()),
            proofCaptureRepository = proofs,
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf(WorkflowDetailViewModel.ARG_WORKFLOW_ID to "wf-1")),
            photoCaptureSource = photos,
            countsRepository = FakeAddCountsRepository(),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.TakePhoto("action-1"))
        advanceUntilIdle()

        assertEquals(
            "one photo must enqueue one completion, got keys ${syncRepository.completeCalls.map { it.idempotencyKey }}",
            1,
            syncRepository.completeCalls.size,
        )
        val refs = syncRepository.completeCalls.single().proofOutboxItems.map { it.outboxItemId }
        assertEquals("the photo rides the completion exactly once", refs.distinct(), refs)
        assertEquals(1, refs.size)
        // Telemetry names what was captured: a photo is not a video.
        val captureEvents = analytics.events.map { it.first }.filter { it.endsWith("_captured") }
        assertEquals(listOf("workflow_photo_captured"), captureEvents)
    }

    /** Realme E2E 2026-09-17: after "Answer kept" the proof step did not show which answer was kept. */
    @Test
    fun `a proof step shows the kept answer as chosen while its photo is still owed`() = runTest(dispatcher) {
        val action = WorkflowActionDto(
            actionId = "action-1",
            actionKey = "kid_suckled",
            seq = 1,
            actionType = "question",
            title = "Kid suckled?",
            status = "pending",
            options = listOf("Yes", "No"),
            proofMinPhotos = 1,
        )
        val workflowsRepository = FakeWorkflowDetailRepository(requiresVideoDetail().copy(actions = listOf(action)))
        val viewModel = buildViewModel(
            workflowsRepository,
            FakeWorkflowDetailSyncRepository(),
            FakeProofCaptureRepository(),
            FakeProofCaptureSource(mutableListOf()),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()
        val yes = viewModel.state.value.actions.single().options.first().value

        viewModel.onEvent(WorkflowDetailEvent.Answer("action-1", yes))
        advanceUntilIdle()

        assertEquals(yes, viewModel.state.value.actions.single().answerValue)
    }

    // -----------------------------------------------------------------------------------------
    // REWORK ROUND (Realme E2E 2026-09-17, P0). A verifier reject clears the step's proof_refs on
    // the server, but this phone still holds the round-1 capture rows. Recovering them as this
    // round's proofs hid the capture button, and re-sent the REJECTED proofs under round 1's key,
    // which collapsed onto the old SUCCEEDED outbox row: the card was stuck.
    // -----------------------------------------------------------------------------------------

    private fun heldStepWrite(
        key: String,
        createdAt: Long,
        opType: String = "WORKFLOW_ACTION_ANSWER",
        status: sg.mesha.goatos.core.data.sync.SyncItemStatus = sg.mesha.goatos.core.data.sync.SyncItemStatus.SUCCEEDED,
        conflict: Boolean = false,
    ) = SyncQueueItem(
        id = "held-$key",
        opType = opType,
        idempotencyKey = key,
        groupKey = "wf-1",
        status = status,
        attemptCount = 1,
        maxAttempts = 5,
        conflict = conflict,
        createdAt = createdAt,
        updatedAt = createdAt,
        lastError = if (conflict) "this action is already completed" else null,
    )

    /** The key round 1 wrote for [actionId] carrying these upload outbox ids (the VM's own formula). */
    private fun roundOneKey(prefix: String, actionId: String, vararg outboxIds: String): String =
        "$prefix:$actionId:" + outboxIds.joinToString(",").hashCode().toUInt().toString(16)

    private fun staleRow(actionId: String, kind: String, ordinal: Int, capturedAtMs: Long) = workflowProofRow(
        id = "stale-$actionId-$kind-$ordinal",
        fieldKey = workflowProofFieldKey(actionId) + "_" + kind + "_" + ordinal,
        serverProofId = "rejected-$actionId-$kind-$ordinal",
        outboxItemId = "stale-outbox-$actionId-$kind-$ordinal",
        capturedAtMs = capturedAtMs,
    )

    private fun photoSource(capturedAtMs: Long) = object : sg.mesha.goatos.capture.PhotoCaptureSource {
        override suspend fun capturePhoto(context: sg.mesha.goatos.capture.PhotoCaptureContext) =
            sg.mesha.goatos.capture.CapturedPhoto(localUri = "file:///reshoot.jpg", capturedAtMs = capturedAtMs)
    }

    private fun reconcileReworkDetail() = requiresVideoDetail().copy(
        module = "reconcile",
        templateKey = "reconcile",
        actions = listOf(
            WorkflowActionDto(
                actionId = "gate", actionKey = "gate_ok", seq = 1, actionType = "question", answerType = "yes_no",
                taskType = "record_yes_no", title = "Gate OK?", status = "rework", proofMinPhotos = 1,
                answerValue = "yes", reworkReason = "Ear tag not visible in the pen video",
            ),
            WorkflowActionDto(
                actionId = "reason", actionKey = "reason", seq = 2, actionType = "question_select",
                title = "Why?", status = "completed", options = listOf("a", "b"), answerValue = "a",
            ),
            WorkflowActionDto(
                actionId = "ret", actionKey = "return_to_pen", seq = 3, actionType = "action", title = "Return to pen",
                status = "rework", blocked = true, blockedReason = "previous_action", proofMinVideos = 1, proofMinPhotos = 1,
            ),
        ),
    )

    @Test
    fun `rework round of a yes-no photo question offers the photo and sends one fresh answer write`() = runTest(dispatcher) {
        val repo = FakeWorkflowDetailRepository(reconcileReworkDetail())
        val sync = FakeWorkflowDetailSyncRepository()
        val gateRoundOne = roundOneKey("wf-answer", "gate", "stale-outbox-gate-photo-1")
        sync.holdOutboxItems(
            heldStepWrite(gateRoundOne, createdAt = 15L),
            heldStepWrite(roundOneKey("wf-complete", "ret", "stale-outbox-ret-video-1", "stale-outbox-ret-photo-1"), 30L, "WORKFLOW_ACTION_COMPLETE"),
            heldStepWrite("wf-complete:ret:f303dbb3", 31L, "WORKFLOW_ACTION_COMPLETE", sg.mesha.goatos.core.data.sync.SyncItemStatus.FAILED, conflict = true),
        )
        val proofs = FakeProofCaptureRepository().apply {
            seedProofs(staleRow("gate", "photo", 1, 10L), staleRow("ret", "video", 1, 20L), staleRow("ret", "photo", 1, 21L))
        }
        val viewModel = buildViewModel(repo, sync, proofs, FakeProofCaptureSource(mutableListOf()), photoSource(100L))
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals("a rejected round's proofs must never be re-sent on open", emptyList<Any>(), sync.answerCalls + sync.completeCalls)
        val gate = viewModel.state.value.actions.first { it.actionId == "gate" }
        assertEquals("the rework step offers Take photo", true, gate.canTakePhoto)
        assertEquals("the rework step offers its choices", true, gate.canAnswer)
        assertEquals("no rejected photo counts toward the re-shoot", 0, gate.proofPhotosCaptured)

        viewModel.onEvent(WorkflowDetailEvent.Answer("gate", "yes"))
        advanceUntilIdle()
        assertEquals("tapping the answer waits for the photo", 0, sync.answerCalls.size)
        assertEquals("tapping the answer is visible", true, viewModel.state.value.message != null)

        viewModel.onEvent(WorkflowDetailEvent.TakePhoto("gate"))
        advanceUntilIdle()

        assertEquals("exactly one answer write for the re-shoot, got ${sync.answerCalls}", 1, sync.answerCalls.size)
        val write = sync.answerCalls.single()
        assertEquals("yes", write.answerValue)
        assertEquals("a round-distinct key, never round 1's SUCCEEDED key", true, write.idempotencyKey != gateRoundOne)
        assertEquals(listOf("proof-outbox-1"), write.proofOutboxItems.map { it.outboxItemId })
    }

    @Test
    fun `rework round of a select photo question keeps its answer and sends one fresh write`() = runTest(dispatcher) {
        val action = WorkflowActionDto(
            actionId = "band", actionKey = "weight_band", seq = 1, actionType = "question_select", title = "Weight band",
            status = "rework", options = listOf("2-3 kg", "3-4 kg"), answerValue = "3-4 kg", proofMinPhotos = 1,
        )
        val repo = FakeWorkflowDetailRepository(requiresVideoDetail().copy(actions = listOf(action)))
        val sync = FakeWorkflowDetailSyncRepository()
        val roundOne = roundOneKey("wf-answer", "band", "stale-outbox-band-photo-1")
        sync.holdOutboxItems(heldStepWrite(roundOne, createdAt = 15L))
        val proofs = FakeProofCaptureRepository().apply { seedProofs(staleRow("band", "photo", 1, 10L)) }
        val viewModel = buildViewModel(repo, sync, proofs, FakeProofCaptureSource(mutableListOf()), photoSource(100L))
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals(true, viewModel.state.value.actions.single().canTakePhoto)
        assertEquals(0, sync.answerCalls.size)

        // The kept answer is pre-selected: the operator only re-shoots the photo.
        viewModel.onEvent(WorkflowDetailEvent.TakePhoto("band"))
        advanceUntilIdle()

        assertEquals("exactly one answer write, got ${sync.answerCalls}", 1, sync.answerCalls.size)
        assertEquals("3-4 kg", sync.answerCalls.single().answerValue)
        assertEquals(true, sync.answerCalls.single().idempotencyKey != roundOne)
        assertEquals(1, sync.answerCalls.single().proofOutboxItems.size)
    }

    @Test
    fun `rework round of a video and photo action records both again and sends one fresh completion`() = runTest(dispatcher) {
        val detail = reconcileReworkDetail().let { d ->
            d.copy(
                actions = d.actions.map {
                    when (it.actionId) {
                        "gate" -> it.copy(status = "completed")
                        "ret" -> it.copy(blocked = false, blockedReason = "")
                        else -> it
                    }
                },
            )
        }
        val repo = FakeWorkflowDetailRepository(detail)
        val sync = FakeWorkflowDetailSyncRepository()
        val roundOne = roundOneKey("wf-complete", "ret", "stale-outbox-ret-video-1", "stale-outbox-ret-photo-1")
        sync.holdOutboxItems(
            heldStepWrite(roundOne, 30L, "WORKFLOW_ACTION_COMPLETE"),
            heldStepWrite("wf-complete:ret:f303dbb3", 31L, "WORKFLOW_ACTION_COMPLETE", sg.mesha.goatos.core.data.sync.SyncItemStatus.FAILED, conflict = true),
        )
        val proofs = FakeProofCaptureRepository().apply {
            seedProofs(staleRow("ret", "video", 1, 20L), staleRow("ret", "photo", 1, 21L))
        }
        val video = FakeProofCaptureSource(mutableListOf(CapturedVideo(localUri = "/proof/ret.mp4", startedAtMs = 100L, endedAtMs = 110L)))
        val viewModel = buildViewModel(repo, sync, proofs, video, photoSource(120L))
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val ret = viewModel.state.value.actions.first { it.actionId == "ret" }
        assertEquals(true, ret.canRecordVideo)
        assertEquals(true, ret.canTakePhoto)
        assertEquals(0, sync.completeCalls.size)

        viewModel.onEvent(WorkflowDetailEvent.RecordVideo("ret"))
        advanceUntilIdle()
        assertEquals("one proof is not the whole step", 0, sync.completeCalls.size)
        viewModel.onEvent(WorkflowDetailEvent.TakePhoto("ret"))
        advanceUntilIdle()

        assertEquals("exactly one completion, got ${sync.completeCalls.map { it.idempotencyKey }}", 1, sync.completeCalls.size)
        val write = sync.completeCalls.single()
        assertEquals(true, write.idempotencyKey != roundOne)
        assertEquals(listOf("video", "photo"), write.proofOutboxItems.map { it.kind })
        assertEquals("only this round's proofs ride the write", true, write.proofOutboxItems.none { it.outboxItemId.startsWith("stale-") })
    }

    @Test
    fun `rework round of a photo-only birth action sends one fresh completion`() = runTest(dispatcher) {
        val action = WorkflowActionDto(
            actionId = "kid", actionKey = "kid_photo", seq = 1, actionType = "action", title = "Kid photo",
            status = "rework", proofMinPhotos = 1, reworkReason = "Blurred",
        )
        val repo = FakeWorkflowDetailRepository(requiresVideoDetail().copy(actions = listOf(action)))
        val sync = FakeWorkflowDetailSyncRepository()
        val roundOne = roundOneKey("wf-complete", "kid", "stale-outbox-kid-photo-1")
        sync.holdOutboxItems(heldStepWrite(roundOne, 15L, "WORKFLOW_ACTION_COMPLETE"))
        val proofs = FakeProofCaptureRepository().apply { seedProofs(staleRow("kid", "photo", 1, 10L)) }
        val viewModel = buildViewModel(repo, sync, proofs, FakeProofCaptureSource(mutableListOf()), photoSource(100L))
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals(0, sync.completeCalls.size)
        assertEquals(true, viewModel.state.value.actions.single().canTakePhoto)

        viewModel.onEvent(WorkflowDetailEvent.TakePhoto("kid"))
        advanceUntilIdle()

        assertEquals(1, sync.completeCalls.size)
        assertEquals(true, sync.completeCalls.single().idempotencyKey != roundOne)
        assertEquals(listOf("proof-outbox-1"), sync.completeCalls.single().proofOutboxItems.map { it.outboxItemId })
    }

    @Test
    fun `a rework step whose earlier write is no longer on the phone never recovers an uploaded proof`() = runTest(dispatcher) {
        val action = WorkflowActionDto(
            actionId = "kid", actionKey = "kid_photo", seq = 1, actionType = "action", title = "Kid photo",
            status = "rework", proofMinPhotos = 1,
        )
        val repo = FakeWorkflowDetailRepository(requiresVideoDetail().copy(actions = listOf(action)))
        val sync = FakeWorkflowDetailSyncRepository() // round 1's SUCCEEDED write was pruned
        val proofs = FakeProofCaptureRepository().apply { seedProofs(staleRow("kid", "photo", 1, 10L)) }
        val viewModel = buildViewModel(repo, sync, proofs, FakeProofCaptureSource(mutableListOf()), photoSource(100L))
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals("an uploaded proof of unknown round is never re-sent as the re-shoot", 0, sync.completeCalls.size)
        assertEquals(true, viewModel.state.value.actions.single().canTakePhoto)
    }

    /**
     * Realme E2E 2026-09-17 (reconcile rework round). The operator re-shot Gate OK; the server took it
     * and the screen read 2 / 3 done, but Return to pen showed no Record video / Take photo and the
     * banner "already saved on this phone with different details" stayed until the card was
     * reopened.
     *
     * The step-write rows the rework rule anchors on come from the sync status WINDOW, which
     * reorders as the re-shoot drains: round 1's Return-to-pen write fell out while an older write
     * for the same step was bumped back in. Recovery re-ran on that emission, judged round 1's
     * Return-to-pen video + photo to be this round's, filled the step's proofs (hiding both capture
     * buttons) and re-sent them under round 1's key, which the phone already held with different
     * details. Recovery exists for a process death before the step write; within one screen's life a
     * capture is tracked by the capture itself, so a step is judged once, when it is first seen.
     */
    @Test
    fun `rework round unlocks the next step live after the re-shot step lands, with no stale re-send`() = runTest(dispatcher) {
        val repo = FakeWorkflowDetailRepository(reconcileReworkDetail())
        val sync = FakeWorkflowDetailSyncRepository()
        val retRoundOne = roundOneKey("wf-complete", "ret", "stale-outbox-ret-video-1", "stale-outbox-ret-photo-1")
        sync.conflictingKeys += retRoundOne
        val olderRetWrite = heldStepWrite("wf-complete:ret:early", createdAt = 5L, opType = "WORKFLOW_ACTION_COMPLETE")
        sync.holdOutboxItems(
            heldStepWrite(roundOneKey("wf-answer", "gate", "stale-outbox-gate-photo-1"), createdAt = 15L),
            heldStepWrite(retRoundOne, createdAt = 30L, opType = "WORKFLOW_ACTION_COMPLETE"),
        )
        val proofs = FakeProofCaptureRepository().apply {
            seedProofs(staleRow("gate", "photo", 1, 10L), staleRow("ret", "video", 1, 20L), staleRow("ret", "photo", 1, 21L))
        }
        val viewModel = buildViewModel(repo, sync, proofs, FakeProofCaptureSource(mutableListOf()), photoSource(100L))
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.TakePhoto("gate"))
        advanceUntilIdle()
        assertEquals("one fresh answer write for the re-shot gate", 1, sync.answerCalls.size)

        // The write lands: the server read now holds Gate OK done and Return to pen open, and the
        // status window has moved -- round 1's Return-to-pen row is out, an older one is back in.
        repo.serverDetail(
            reconcileReworkDetail().let { d ->
                d.copy(
                    actions = d.actions.map {
                        when (it.actionId) {
                            "gate" -> it.copy(status = "completed")
                            "ret" -> it.copy(blocked = false, blockedReason = "")
                            else -> it
                        }
                    },
                )
            },
        )
        sync.holdOutboxItems(
            olderRetWrite,
            heldStepWrite(sync.answerCalls.single().idempotencyKey, createdAt = 110L),
        )
        advanceUntilIdle()

        val state = viewModel.state.value
        assertEquals("2 of 3 steps done", 2, state.actionsDone)
        val ret = state.actions.first { it.actionId == "ret" }
        assertEquals("Return to pen offers Record video without reopening", true, ret.canRecordVideo)
        assertEquals("Return to pen offers Take photo without reopening", true, ret.canTakePhoto)
        assertEquals("no rejected round's proof counts toward the new round", 0, ret.proofVideosCaptured + ret.proofPhotosCaptured)
        assertEquals("round 1's proofs are never re-sent", emptyList<Any>(), sync.completeCalls.map { it.idempotencyKey })
        assertEquals("no stale banner on screen", null, state.message)
    }

    @Test
    fun `terminal complete failure rolls optimistic action back to pending`() = runTest(dispatcher) {
        val workflowsRepository = FakeWorkflowDetailRepository(
            requiresVideoDetail().copy(actions = requiresVideoDetail().actions.map { it.copy(requiresVideo = false) }),
        )
        val syncRepository = FakeWorkflowDetailSyncRepository()
        val viewModel = buildViewModel(
            workflowsRepository,
            syncRepository,
            FakeProofCaptureRepository(),
            FakeProofCaptureSource(mutableListOf()),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.Complete("action-1"))
        advanceUntilIdle()
        assertEquals("completed", workflowsRepository.actionStatus("action-1"))

        syncRepository.emitTerminalFailure("wf-complete:action-1", "Completion was rejected.")
        advanceUntilIdle()

        assertEquals("pending", workflowsRepository.actionStatus("action-1"))
    }

    @Test
    fun `terminal answer failure rolls optimistic answer back to pending`() = runTest(dispatcher) {
        val detail = requiresVideoDetail().copy(
            actions = requiresVideoDetail().actions.map {
                it.copy(actionType = "question", requiresVideo = false, options = listOf("Yes", "No"))
            },
        )
        val workflowsRepository = FakeWorkflowDetailRepository(detail)
        val syncRepository = FakeWorkflowDetailSyncRepository()
        val viewModel = buildViewModel(
            workflowsRepository,
            syncRepository,
            FakeProofCaptureRepository(),
            FakeProofCaptureSource(mutableListOf()),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkflowDetailEvent.Answer("action-1", "Yes"))
        advanceUntilIdle()
        assertEquals("completed", workflowsRepository.actionStatus("action-1"))

        syncRepository.emitTerminalFailure("wf-answer:action-1", "Answer was rejected.")
        advanceUntilIdle()

        assertEquals("pending", workflowsRepository.actionStatus("action-1"))
    }
}

private fun workflowProofRow(
    id: String,
    fieldKey: String,
    serverProofId: String?,
    outboxItemId: String?,
    capturedAtMs: Long,
) = ProofCaptureRow(
    id = id,
    fieldKey = fieldKey,
    proofSubject = ProofSubject.GOAT,
    subjectId = "goat-1",
    localUri = "/proof/$id.mp4",
    mimeType = "video/mp4",
    caption = "Record kid video",
    capturedAtMs = capturedAtMs,
    capturedStartMs = capturedAtMs,
    capturedEndMs = capturedAtMs + 1,
    capturedByPrincipalId = null,
    syncStatus = CaptureSyncStatus.SYNCED,
    serverProofId = serverProofId,
    outboxItemId = outboxItemId,
    lastError = null,
)

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

    /** Installs a fresh server read, as refreshDetail's reconcile would. */
    fun serverDetail(detail: WorkflowDetailResponseDto) {
        detailFlow.value = detail
    }

    fun actionStatus(actionId: String): String? =
        detailFlow.value?.actions?.firstOrNull { it.actionId == actionId }?.status

    override suspend fun rollbackAction(workflowId: String, actionId: String) {
        detailFlow.value = detailFlow.value?.let { detail ->
            detail.copy(actions = detail.actions.map { if (it.actionId == actionId) it.copy(status = "pending", answerValue = null) else it })
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
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()
    override fun observeStatus() = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> =
        items.getOrPut(itemId) { MutableStateFlow(null) }

    data class CompleteCall(
        val groupKey: String,
        val idempotencyKey: String,
        val workflowId: String,
        val actionId: String,
        val proofOutboxItemId: String?,
        val proofOutboxItems: List<WorkflowProofOutboxRef>,
    )
    data class AnswerCall(
        val groupKey: String,
        val idempotencyKey: String,
        val workflowId: String,
        val actionId: String,
        val answerValue: String,
        val proofOutboxItemId: String?,
        val proofOutboxItems: List<WorkflowProofOutboxRef> = emptyList(),
    )

    /** Seeds the outbox rows this phone already holds (e.g. an earlier round's SUCCEEDED step write). */
    fun holdOutboxItems(vararg held: SyncQueueItem) {
        status.value = status.value.copy(items = held.toList())
    }

    val completeCalls = mutableListOf<CompleteCall>()
    /** Keys already held on this phone by a write with different details (a local fingerprint conflict). */
    val conflictingKeys = mutableSetOf<String>()
    /** Production's enqueue suspends on Room I/O; true models that suspension. */
    var suspendOnEnqueue = false
    val answerCalls = mutableListOf<AnswerCall>()
    // key -> the single outbox item id ever minted for it (idempotent collapse).
    private val outboxItemIdByKey = mutableMapOf<String, String>()
    private var nextOutboxId = 0

    fun completeCallsByKey(key: String) = completeCalls.filter { it.idempotencyKey == key }
    fun outboxItemIdsForKey(key: String): Set<String> = setOfNotNull(outboxItemIdByKey[key])

    fun emitTerminalFailure(idempotencyKey: String, message: String) {
        val id = outboxItemIdByKey.getValue(idempotencyKey)
        items.getOrPut(id) { MutableStateFlow(null) }.value = SyncQueueItem(
            id = id,
            opType = "WORKFLOW_ACTION_COMPLETE",
            idempotencyKey = idempotencyKey,
            groupKey = "wf-1",
            status = sg.mesha.goatos.core.data.sync.SyncItemStatus.FAILED,
            attemptCount = 1,
            maxAttempts = 3,
            conflict = true,
            createdAt = 1L,
            updatedAt = 2L,
            lastError = message,
        )
    }

    override suspend fun enqueueWorkflowActionAnswer(
        groupKey: String,
        idempotencyKey: String,
        workflowId: String,
        actionId: String,
        answerValue: String,
        proofOutboxItemId: String?,
        proofOutboxItems: List<WorkflowProofOutboxRef>,
    ): AppResult<String> {
        answerCalls += AnswerCall(groupKey, idempotencyKey, workflowId, actionId, answerValue, proofOutboxItemId, proofOutboxItems)
        val id = outboxItemIdByKey.getOrPut(idempotencyKey) { "wf-outbox-${nextOutboxId++}" }
        return AppResult.Ok(id)
    }

    override suspend fun enqueueWorkflowActionComplete(
        groupKey: String,
        idempotencyKey: String,
        workflowId: String,
        actionId: String,
        proofOutboxItemId: String?,
        proofOutboxItems: List<WorkflowProofOutboxRef>,
    ): AppResult<String> {
        if (suspendOnEnqueue) repeat(20) { kotlinx.coroutines.yield() }
        completeCalls += CompleteCall(groupKey, idempotencyKey, workflowId, actionId, proofOutboxItemId, proofOutboxItems)
        if (idempotencyKey in conflictingKeys) {
            return AppResult.Err("This was already saved on this phone with different details. Open Sync status to review it.")
        }
        val id = outboxItemIdByKey.getOrPut(idempotencyKey) { "wf-outbox-${nextOutboxId++}" }
        return AppResult.Ok(id)
    }

    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = AppResult.Ok("proof-outbox-1")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
