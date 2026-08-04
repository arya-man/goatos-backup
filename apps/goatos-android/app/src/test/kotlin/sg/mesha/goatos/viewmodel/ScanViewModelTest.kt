package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import android.view.KeyEvent
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.ChannelBackedProofCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.capture.ROSTER_SCAN_FIELD_KEY
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.RfidScanAttemptOutcome
import sg.mesha.goatos.core.data.capture.RfidScanTagRole
import sg.mesha.goatos.core.data.forms.FormField
import sg.mesha.goatos.core.data.forms.FormFieldType
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterRowDto
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskPresentationDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanError
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.rfid.FakeScanSource
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

@OptIn(ExperimentalCoroutinesApi::class)
class ScanViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `scan screen RFID hit is persisted and included in submit payload`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("TAG-100")
        advanceUntilIdle()

        assertEquals(listOf("TAG-100"), scanCaptures.tagsForTask("task-1"))
        assertEquals(1L, scanCaptures.rowsForTask("task-1").single().capturedAtMs)
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED), scanAttempts.calls.map { it.outcome })
        assertEquals(ScanStatus.DONE, scanVm.state.value.roster.single().status)

        val submitSync = CapturingSubmitSyncRepository()
        val proofRepo = FakeProofCaptureRepository()
        val capturedProof = proofRepo.capture(
            taskId = "task-1",
            fieldKey = "vaccination_goat_proof",
            subject = ProofSubject.GOAT,
            subjectId = "goat-1",
            localUri = "file://goat-1.mp4",
            mimeType = "video/mp4",
            caption = null,
            scopeType = "task",
            scopeId = "task-1",
            capturedStartMs = 1_000,
            capturedEndMs = 2_000,
            capturedByPrincipalId = "operator-1",
        ) as AppResult.Ok
        proofRepo.markSynced(capturedProof.value.id, "proof-1")
        val submitVm = SubmitViewModel(
            repo = FakeTaskRepository(
                task = TaskSummaryDto(taskId = "task-1", sopVersionId = "sop-1", scopeId = "shed-1", title = "Shed 1"),
                form = FormSpec(
                    schemaVersion = "goatos.sop-form.v1",
                    fields = listOf(FormField(key = "goat_ids", label = "Goats", type = FormFieldType.GOAT_SCAN, required = true)),
                    rules = emptyList(),
                ),
            ),
            syncRepository = submitSync,
            scanCaptureRepository = scanCaptures,
            proofCaptureRepository = proofRepo,
            scanSource = FakeScanSource(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            savedStateHandle = SavedStateHandle(mapOf("taskId" to "task-1")),
        )
        backgroundScope.launch { submitVm.state.collect {} }
        advanceUntilIdle()

        assertTrue("scan-screen rows must satisfy the required GOAT_SCAN field", submitVm.state.value.canSubmit)
        submitVm.onEvent(SubmitEvent.Submit)
        advanceUntilIdle()

        val answer = submitSync.lastRequest?.answers?.get("goat_ids") as? JsonArray
        assertEquals(listOf(JsonPrimitive("goat-1")), answer)
    }

    @Test
    fun `persisted RFID capture restores done state after process recreation`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        scanCaptures.recordScan(
            taskId = "task-1",
            fieldKey = ROSTER_SCAN_FIELD_KEY,
            tag = "TAG-100",
            goatId = "goat-1",
            obligationId = "obl-1",
        )
        val scanAttempts = FakeScanAttemptRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = FakeRfidReaderPort()

        // A new ViewModel represents a recreated process. No transient _localDone state exists.
        val recreated = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = proofSource,
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { recreated.state.collect {} }
        advanceUntilIdle()

        assertEquals(ScanStatus.DONE, recreated.state.value.roster.single().status)
        assertEquals(1, recreated.state.value.doneCount)
        assertEquals(0, recreated.state.value.pendingCount)

        reader.emit("TAG-100")
        advanceUntilIdle()

        assertEquals("restored proof rescan must repair the durable scan capture idempotently", 2, scanCaptures.recordScanCalls)
        assertEquals("proof-missing restored evidence must reopen the proof path", 1, proofSource.captureCount)
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED), scanAttempts.calls.map { it.outcome })
        assertEquals("proof_rescan", scanAttempts.calls.single().reason)
    }

    @Test
    fun `scan header title uses task shed name plus scan`() = runTest(dispatcher) {
        val vm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
            ),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = FakeScanCaptureRepository(),
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(
                TaskDetail(
                    task = TaskSummaryDto(
                        taskId = "task-1",
                        title = "Generic vaccination cohort",
                        presentation = TaskPresentationDto(title = "Park"),
                    ),
                    form = FormSpec.Empty,
                ),
            ),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    "shedId" to "shed-1",
                    "taskId" to "task-1",
                    "scanTitle" to "Gandhi 1 - Part 3",
                ),
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals("Gandhi 1 - Part 3 Scan", vm.state.value.cohortLabel)
    }

    @Test
    fun `repeated RFID scan is not recorded as another capture`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val proofRepo = FakeProofCaptureRepository()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = proofRepo,
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("TAG-100")
        advanceUntilIdle()
        seedSyncedProof(proofRepo, "goat-1")
        advanceUntilIdle()
        reader.emit("TAG-100")
        advanceUntilIdle()

        assertEquals(listOf("TAG-100"), scanCaptures.tagsForTask("task-1"))
        assertEquals("duplicate hardware reads should not re-record the same roster tag", 1, scanCaptures.recordScanCalls)
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED, RfidScanAttemptOutcome.DUPLICATE), scanAttempts.calls.map { it.outcome })
        assertEquals("already scanned duplicate scans are a notice, not another visible feed row", 1, scanVm.state.value.feed.size)
        assertEquals("Already scanned · ET", scanVm.state.value.duplicateNotice)
        assertEquals(ScanStatus.DONE, scanVm.state.value.roster.single().status)
    }

    @Test
    fun `scanning a second goat while the first goat's proof video is still recording cancels the first camera and reopens bound to the second goat, so a completed recording can never save under the wrong subject id`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(
                    rows = listOf(
                        scanRow("goat-1", "TAG-100", "obl-1"),
                        scanRow("goat-2", "TAG-200", "obl-2"),
                    ),
                ),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = proofRepo,
            proofCaptureSource = proofSource,
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        // Goat A is scanned; its camera opens and the recording is still in progress (suspended
        // on the gate, i.e. captureVideo() has NOT returned — nothing has been written to Room or
        // the outbox yet) when the operator walks to goat B and scans it.
        val goatAGate = proofSource.queueGate()
        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals("goat A's camera opened", 1, proofSource.captureCount)
        assertEquals(0, proofRepo.captureCalls.size)

        val goatBGate = proofSource.queueGate()
        reader.emit("TAG-200")
        advanceUntilIdle()

        // Goat A's still-recording (unfinished) camera session is cancelled and a NEW camera opens
        // bound to goat B — the operator is standing at B, so B is what the reopened camera sees.
        assertEquals("goat B's scan must cancel A's unfinished capture and open a fresh camera for B", 2, proofSource.captureCount)
        assertEquals("no proof may be written until a capture actually completes", 0, proofRepo.captureCalls.size)
        assertEquals("TAG-100 still needs its video.", scanVm.state.value.duplicateNotice)

        // Goat A's original (cancelled) gate completing now must NOT deliver a proof — the coroutine
        // that owned it was cancelled and its result is discarded.
        goatAGate.complete(CapturedVideo(localUri = "file://goat-a.mp4", startedAtMs = 1, endedAtMs = 2))
        advanceUntilIdle()
        assertEquals("a cancelled capture's late result must never be saved", 0, proofRepo.captureCalls.size)

        // Goat B's recording finishes: the proof that lands is genuinely B's footage, saved under
        // B's subject id. No path exists where A's cancelled recording could land under B's id, or
        // vice versa.
        goatBGate.complete(CapturedVideo(localUri = "file://goat-b.mp4", startedAtMs = 3, endedAtMs = 4))
        advanceUntilIdle()

        assertEquals(1, proofRepo.captureCalls.size)
        assertEquals("goat-2", proofRepo.captureCalls.single().subjectId)
        assertEquals("file://goat-b.mp4", proofRepo.captureCalls.single().localUri)
    }

    @Test
    fun `goat A is left needing proof, not falsely marked done or uploading, after its capture is cancelled for goat B`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(
                    rows = listOf(
                        scanRow("goat-1", "TAG-100", "obl-1"),
                        scanRow("goat-2", "TAG-200", "obl-2"),
                    ),
                ),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = proofRepo,
            proofCaptureSource = proofSource,
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        val goatAGate = proofSource.queueGate()
        reader.emit("TAG-100")
        advanceUntilIdle()

        proofSource.queueGate()
        reader.emit("TAG-200")
        advanceUntilIdle()

        val goatARow = scanVm.state.value.roster.first { it.goatId == "goat-1" }
        assertEquals("goat A stays DONE (it really was scanned) but must not read as proof-synced", ScanStatus.DONE, goatARow.status)
        assertEquals("cancelled-before-capture goat A must not read as proof-synced", sg.mesha.goatos.feature.scan.ProofUploadStatus.MISSING, goatARow.proofUploadStatus)
        assertFalse(
            "goat A must not keep an optimistic uploading marker for a capture that never completed",
            goatARow.evidenceUploading,
        )
        assertTrue("no proof was ever written for goat A", proofRepo.captureCalls.none { it.subjectId == "goat-1" })

        // Not a dangling reference either: goat A's gate completing late changes nothing about A.
        goatAGate.complete(CapturedVideo(localUri = "file://goat-a-late.mp4", startedAtMs = 1, endedAtMs = 2))
        advanceUntilIdle()
        val goatARowAfter = scanVm.state.value.roster.first { it.goatId == "goat-1" }
        assertEquals(sg.mesha.goatos.feature.scan.ProofUploadStatus.MISSING, goatARowAfter.proofUploadStatus)
        assertFalse(goatARowAfter.evidenceUploading)
    }

    /**
     * Defect 1 of the wrong-goat capture fix, one layer down. The two tests above use
     * [FakeProofCaptureSource]'s per-call gates, which isolate every capture from every other and
     * so cannot express the production mechanism at all. Production shares ONE buffered result
     * channel across every capture request (`VideoCaptureLauncher.kt:38/52/87`), so goat A's
     * recording — finalized asynchronously by CameraX AFTER the operator turned to goat B and
     * scanned — is left in that buffer and handed straight to goat B's capture. Goat A's clip then
     * becomes goat B's medical proof, and nothing downstream can tell.
     */
    @Test
    fun `a cancelled capture's finished recording is never handed to the goat scanned next`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = ChannelBackedProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(
                    rows = listOf(
                        scanRow("goat-1", "TAG-100", "obl-1"),
                        scanRow("goat-2", "TAG-200", "obl-2"),
                    ),
                ),
            ),
            reader = reader,
            scanCaptureRepository = FakeScanCaptureRepository(),
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = proofRepo,
            proofCaptureSource = proofSource,
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        // Goat A is scanned; the camera opens. The operator presses stop, but CameraX finalization
        // is asynchronous — no result has been delivered yet.
        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals("goat A's camera opened", 1, proofSource.captureCount)

        // The operator turns to goat B and scans it. A's unfinished capture is cancelled, a fresh
        // camera opens for B.
        reader.emit("TAG-200")
        advanceUntilIdle()
        assertEquals("a fresh camera must open for goat B", 2, proofSource.captureCount)

        // NOW A's recording finalizes. It belongs to request #1, which was cancelled.
        proofSource.deliverRecorderResult(
            requestOrdinal = 1,
            video = CapturedVideo(localUri = "file://goat-a.mp4", startedAtMs = 1, endedAtMs = 2),
        )
        advanceUntilIdle()
        assertEquals(
            "goat A's clip must never be delivered to goat B's capture and saved as B's proof",
            0,
            proofRepo.captureCalls.size,
        )

        // Goat B's own recording still lands correctly — a stale result must not be able to
        // consume B's turn either.
        proofSource.deliverRecorderResult(
            requestOrdinal = 2,
            video = CapturedVideo(localUri = "file://goat-b.mp4", startedAtMs = 3, endedAtMs = 4),
        )
        advanceUntilIdle()
        assertEquals(1, proofRepo.captureCalls.size)
        assertEquals("goat-2", proofRepo.captureCalls.single().subjectId)
        assertEquals("file://goat-b.mp4", proofRepo.captureCalls.single().localUri)
    }

    /**
     * Regression guard for the A -> B -> A rescan, where the cancelled capture's `finally` was
     * guarded by SUBJECT identity rather than JOB identity: on the third scan `proofCaptureGoatId`
     * is goat A once more, so a stale job's cleanup could alias the LIVE second capture of the
     * same goat — releasing its in-flight ownership (a later scan would then open a second
     * concurrent camera instead of being refused) and stranding its row as "uploading".
     *
     * HONEST NOTE: this test passes both before and after the job-identity guard. On the only
     * cancel path that exists (`requestGoatProof` running on `Dispatchers.Main.immediate`, the
     * capture suspended in the relay), `cancel()` resumes the cancelled continuation UNDISPATCHED,
     * so the stale `finally` runs synchronously inside `cancel()` — before `proofCaptureGoatId` is
     * reassigned — and the aliasing window never opens. The guard was still changed to job
     * identity because it is the property actually meant (a capture, not an animal, owns the
     * camera) and it removes the hazard rather than depending on a dispatcher's inlining rule.
     * This test locks in the observable behaviour the aliasing would have broken.
     */
    @Test
    fun `a stale cancelled job must not tear down the live capture that superseded it after an A to B to A rescan`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = ChannelBackedProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(
                    rows = listOf(
                        scanRow("goat-1", "TAG-100", "obl-1"),
                        scanRow("goat-2", "TAG-200", "obl-2"),
                        scanRow("goat-3", "TAG-300", "obl-3"),
                    ),
                ),
            ),
            reader = reader,
            scanCaptureRepository = FakeScanCaptureRepository(),
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = proofRepo,
            proofCaptureSource = proofSource,
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("TAG-100") // capture #1, goat A
        reader.emit("TAG-200") // capture #2, goat B — cancels #1
        reader.emit("TAG-100") // capture #3, goat A again — cancels #2
        advanceUntilIdle()
        assertEquals("each different-goat scan retargets the camera", 3, proofSource.captureCount)

        // Capture #3 is the LIVE one and capture #1 (same goat) is dead. If the dead job's cleanup
        // was allowed to run — because it is guarded by SUBJECT id, which goat A now matches again
        // — it releases the live capture's in-flight ownership, and this duplicate read of the
        // animal currently in front of the camera opens a SECOND concurrent camera instead of
        // being refused.
        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals(
            "re-scanning the goat whose camera is live must be refused, not open a second concurrent camera",
            3,
            proofSource.captureCount,
        )
        assertEquals("Finish the current animal's video first.", scanVm.state.value.duplicateNotice)

        // Scanning a third goat now must cancel #3 and open exactly one new camera.
        reader.emit("TAG-300")
        advanceUntilIdle()
        assertEquals("goat C must retarget the live camera, not open a second concurrent one", 4, proofSource.captureCount)

        // #3 was cancelled by C, so its late result belongs to nobody; C's is the live one.
        proofSource.deliverRecorderResult(
            requestOrdinal = 3,
            video = CapturedVideo(localUri = "file://goat-a-2.mp4", startedAtMs = 5, endedAtMs = 6),
        )
        advanceUntilIdle()
        assertTrue(
            "a cancelled capture's clip must never be saved for the goat scanned after it",
            proofRepo.captureCalls.none { it.subjectId == "goat-3" },
        )

        proofSource.deliverRecorderResult(
            requestOrdinal = 4,
            video = CapturedVideo(localUri = "file://goat-c.mp4", startedAtMs = 7, endedAtMs = 8),
        )
        advanceUntilIdle()
        assertEquals(1, proofRepo.captureCalls.size)
        assertEquals("goat-3", proofRepo.captureCalls.single().subjectId)
        assertEquals("file://goat-c.mp4", proofRepo.captureCalls.single().localUri)

        // And no goat whose capture was CANCELLED is left stranded showing "uploading" — that is
        // the other half of the stale-cleanup defect (the live job's own guard fails after the
        // stale one has already cleared the state it needed).
        val stuck = scanVm.state.value.roster.filter { it.goatId != "goat-3" && it.evidenceUploading }
        assertTrue("no cancelled goat may be stuck showing 'uploading': $stuck", stuck.isEmpty())
    }

    @Test
    fun `the busy proof-capture notice clears once the busy condition resolves, without waiting for an unrelated next scan`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(
                    rows = listOf(scanRow("goat-1", "TAG-100", "obl-1")),
                ),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = proofRepo,
            proofCaptureSource = proofSource,
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        // Goat A's camera opens and is still recording (suspended on the gate).
        val goatAGate = proofSource.queueGate()
        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals(1, proofSource.captureCount)

        // The SAME tag is re-scanned while its own capture is still in flight (e.g. a duplicate
        // hardware read of the animal that's already being recorded) — this must be refused
        // visibly rather than opening a second camera for the same animal.
        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals("re-scanning the goat already being recorded must not open a second camera", 1, proofSource.captureCount)
        assertEquals("Finish the current animal's video first.", scanVm.state.value.duplicateNotice)

        // The recording finishes normally. The busy condition has resolved — the notice must clear
        // on its own, not linger until some unrelated future scan event clears it.
        goatAGate.complete(CapturedVideo(localUri = "file://goat-a.mp4", startedAtMs = 1, endedAtMs = 2))
        advanceUntilIdle()

        assertNull(
            "the busy notice must clear once the capture it referred to finishes, without needing another scan",
            scanVm.state.value.duplicateNotice,
        )
        assertEquals(1, proofRepo.captureCalls.size)
        assertEquals("goat-1", proofRepo.captureCalls.single().subjectId)
    }

    @Test
    fun `secondary tag for same goat records duplicate attempt but does not count twice`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val proofRepo = FakeProofCaptureRepository()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(
                    rows = listOf(scanRow("goat-1", "901007000504418", "obl-1", secondaryTag = "901007000504419")),
                ),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = proofRepo,
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("901007000504418")
        advanceUntilIdle()
        seedSyncedProof(proofRepo, "goat-1")
        advanceUntilIdle()
        reader.emit("901007000504419")
        advanceUntilIdle()

        assertEquals(listOf("901007000504418"), scanCaptures.tagsForTask("task-1"))
        assertEquals(1, scanCaptures.recordScanCalls)
        assertEquals(
            listOf(RfidScanAttemptOutcome.ACCEPTED, RfidScanAttemptOutcome.DUPLICATE),
            scanAttempts.calls.map { it.outcome },
        )
        assertEquals(listOf(RfidScanTagRole.PRIMARY, RfidScanTagRole.SECONDARY), scanAttempts.calls.map { it.tagRole })
        assertEquals("goat-1", scanAttempts.calls[1].goatId)
        assertEquals("goat_already_scanned", scanAttempts.calls[1].reason)
        assertEquals(ScanStatus.DONE, scanVm.state.value.roster.single().status)
        assertEquals(1, scanVm.state.value.doneCount)
        assertEquals("secondary duplicate stays out of the visible scan list", 1, scanVm.state.value.feed.size)
        assertEquals("Already scanned · ET", scanVm.state.value.duplicateNotice)
    }

    @Test
    fun `synced proof replacement requires explicit arm and same RFID rescan`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = proofRepo,
            proofCaptureSource = proofSource,
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("TAG-100")
        advanceUntilIdle()
        seedSyncedProof(proofRepo, "goat-1")
        advanceUntilIdle()

        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals("unarmed duplicate must not open replacement camera", 1, proofSource.captureCount)
        assertEquals("goat_already_scanned", scanAttempts.calls.last().reason)

        proofSource.queue(CapturedVideo(localUri = "file://replacement.mp4", startedAtMs = 10, endedAtMs = 20))
        scanVm.onEvent(ScanEvent.ArmProofReplacement("goat-1"))
        advanceUntilIdle()
        reader.emit("TAG-100")
        advanceUntilIdle()

        assertEquals("armed same-tag scan opens replacement camera", 2, proofSource.captureCount)
        assertEquals("proof_replace_requested", scanAttempts.calls.last().reason)
        assertEquals(RfidScanAttemptOutcome.DUPLICATE, scanAttempts.calls.last().outcome)
        assertEquals("file://replacement.mp4", proofRepo.captureCalls.last().localUri)
        assertEquals(1, scanCaptures.recordScanCalls)
    }

    @Test
    fun `latest replace arm wins and stale duplicate notice is cleared`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(
                    rows = listOf(
                        scanRow("goat-1", "TAG-100", "obl-1").copy(status = "done"),
                        scanRow("goat-2", "TAG-200", "obl-2").copy(status = "done"),
                    ),
                ),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = proofRepo,
            proofCaptureSource = proofSource,
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()
        seedSyncedProof(proofRepo, "goat-1")
        seedSyncedProof(proofRepo, "goat-2")
        advanceUntilIdle()

        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals("Already scanned · ET", scanVm.state.value.duplicateNotice)

        scanVm.onEvent(ScanEvent.ArmProofReplacement("goat-1"))
        advanceUntilIdle()
        scanVm.onEvent(ScanEvent.ArmProofReplacement("goat-2"))
        advanceUntilIdle()
        assertNull("replace intent should not share the duplicate notice strip", scanVm.state.value.duplicateNotice)
        assertEquals("goat-2", scanVm.state.value.proofReplacementGoatId)

        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals("wrong armed tag should stay duplicate, not replace", "goat_already_scanned", scanAttempts.calls.last().reason)
        assertEquals(0, proofSource.captureCount)
        assertNull(scanVm.state.value.proofReplacementGoatId)

        scanVm.onEvent(ScanEvent.ArmProofReplacement("goat-2"))
        proofSource.queue(CapturedVideo(localUri = "file://right.mp4", startedAtMs = 30, endedAtMs = 40))
        reader.emit("TAG-200")
        advanceUntilIdle()

        assertEquals("proof_replace_requested", scanAttempts.calls.last().reason)
        assertEquals("file://right.mp4", proofRepo.captureCalls.last().localUri)
    }

    @Test
    fun `rescan of proof missing goat repairs durable roster capture`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("TAG-100")
        advanceUntilIdle()
        reader.emit("TAG-100")
        advanceUntilIdle()

        assertEquals(listOf("TAG-100"), scanCaptures.tagsForTask("task-1"))
        assertEquals("proof rescan must repair the durable scan capture idempotently", 2, scanCaptures.recordScanCalls)
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED, RfidScanAttemptOutcome.ACCEPTED), scanAttempts.calls.map { it.outcome })
        assertEquals("proof_rescan", scanAttempts.calls[1].reason)
        assertNull("proof rescan is not a duplicate notice", scanVm.state.value.duplicateNotice)
    }

    @Test
    fun `manual goat tap is draft-only until a physical RFID read supplies durable evidence`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val reader = FakeRfidReaderPort()
        val scanVm = ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
            ),
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        scanVm.onEvent(ScanEvent.Tap)
        advanceUntilIdle()

        // R50-024: a manual ring tap only advances the draft UI overlay. It must never fabricate
        // an accepted RFID attempt or a durable roster scan/outbox write — only a subsequent
        // physical reader event may supply that evidence.
        assertEquals(ScanStatus.DONE, scanVm.state.value.roster.single().status)
        assertEquals(emptyList<String>(), scanCaptures.tagsForTask("task-1"))
        assertTrue(scanAttempts.calls.isEmpty())

        reader.emit("TAG-100")
        advanceUntilIdle()

        // A real reader hit on the same tag now replaces the draft with durable evidence: exactly
        // one accepted attempt and one roster-scan capture, not a duplicate/second recording.
        assertEquals(listOf("TAG-100"), scanCaptures.tagsForTask("task-1"))
        assertEquals(
            listOf(RfidScanAttemptOutcome.ACCEPTED),
            scanAttempts.calls.map { it.outcome },
        )
    }

    @Test
    fun `roster scan capture is not fanned out across multiple scan fields`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        scanCaptures.recordScan(taskId = "task-1", fieldKey = ROSTER_SCAN_FIELD_KEY, tag = "TAG-100")
        val submitSync = CapturingSubmitSyncRepository()
        val submitVm = SubmitViewModel(
            repo = FakeTaskRepository(
                task = TaskSummaryDto(taskId = "task-1", sopVersionId = "sop-1", scopeId = "shed-1", title = "Shed 1"),
                form = FormSpec(
                    schemaVersion = "goatos.sop-form.v1",
                    fields = listOf(
                        FormField(key = "goat_ids", label = "Vaccinated goats", type = FormFieldType.GOAT_SCAN, required = false),
                        FormField(key = "witness_goat_ids", label = "Witness goats", type = FormFieldType.GOAT_SCAN, required = false),
                    ),
                    rules = emptyList(),
                ),
            ),
            syncRepository = submitSync,
            scanCaptureRepository = scanCaptures,
            proofCaptureRepository = FakeProofCaptureRepository(),
            scanSource = FakeScanSource(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            savedStateHandle = SavedStateHandle(mapOf("taskId" to "task-1")),
        )
        backgroundScope.launch { submitVm.state.collect {} }
        advanceUntilIdle()

        assertTrue("optional scan fields should not block submission", submitVm.state.value.canSubmit)
        submitVm.onEvent(SubmitEvent.Submit)
        advanceUntilIdle()

        assertTrue("submission should still be enqueued", submitSync.lastRequest != null)
        assertEquals(null, submitSync.lastRequest?.answers?.get("goat_ids"))
        assertEquals(null, submitSync.lastRequest?.answers?.get("witness_goat_ids"))
    }

    @Test
    fun `scan screen warms the full shed roster before accepting page two tag reads`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val reader = FakeRfidReaderPort()
        val repo = FakeScanExecutionRepository(
            firstPage = ScanRosterResponseDto(
                rows = listOf(scanRow("goat-1", "TAG-001", "obl-1")),
                nextCursor = "cursor-2",
            ),
            continuationPages = mapOf(
                "cursor-2" to ScanRosterResponseDto(
                    rows = listOf(scanRow("goat-2", "TAG-200", "obl-2")),
                    nextCursor = null,
                ),
            ),
        )
        val vm = ScanViewModel(
            repo = repo,
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(listOf("TAG-001", "TAG-200"), vm.state.value.roster.map { it.primaryTag })
        assertFalse("scan screen must not accept RFID until the complete shed roster is cached", vm.state.value.hasMore)
        assertTrue("operator can scan once the complete roster is cached locally", vm.state.value.scanEnabled)

        reader.emit("TAG-200")
        advanceUntilIdle()

        val loadedPageTwoRow = vm.state.value.roster.single { it.primaryTag == "TAG-200" }
        assertEquals(ScanStatus.DONE, loadedPageTwoRow.status)
        assertEquals(listOf("TAG-200"), scanCaptures.tagsForTask("task-1"))
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED), scanAttempts.calls.map { it.outcome })
        assertFalse(vm.state.value.feed.any { it.status == ScanStatus.SKIPPED && it.primaryTag == "TAG-200" })
    }

    @Test
    fun `warm cached roster stays scannable while background refresh is in flight`() = runTest(dispatcher) {
        val refreshStarted = CompletableDeferred<Unit>()
        val refreshGate = CompletableDeferred<Unit>()
        val reader = FakeRfidReaderPort()
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val repo = FakeScanExecutionRepository(
            firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-001", "obl-1"))),
            warmCache = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-001", "obl-1"))),
            refreshStarted = refreshStarted,
            refreshGate = refreshGate,
        )
        val vm = ScanViewModel(
            repo = repo,
            reader = reader,
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = scanAttempts,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        refreshStarted.await()
        advanceUntilIdle()

        assertTrue("warm Room roster should stay visible during refresh", vm.state.value.roster.isNotEmpty())
        assertTrue("background refresh must not disable RFID input over a warm Room roster", vm.state.value.scanEnabled)
        assertTrue(vm.state.value.isRefreshing)

        reader.emit("TAG-001")
        advanceUntilIdle()

        assertEquals(listOf("TAG-001"), scanCaptures.tagsForTask("task-1"))
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED), scanAttempts.calls.map { it.outcome })

        refreshGate.complete(Unit)
        advanceUntilIdle()
    }

    // --- Submit proof gate over the FULL shed roster (option 2) -------------------------------------

    @Test
    fun `all done animals with synced proofs allow submit`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val vm = proofGateVm(doneRosterRepo(3), proofRepo)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        assertEquals(0, vm.state.value.pendingCount)

        (1..3).forEach { seedSyncedProof(proofRepo, "goat-$it") }
        advanceUntilIdle()

        assertTrue("all done animals proof-synced ⇒ submit allowed", vm.state.value.canSubmit)
        assertTrue(vm.state.value.proofActionNeeded.isEmpty())
    }

    @Test
    fun `refresh writes shed-specific completion summary used by proof gate`() = runTest(dispatcher) {
        val tasks = FakeTasksRepositoryForCapture(
            detail = TaskDetail(
                task = TaskSummaryDto(taskId = "task-1", scopeType = "shed", scopeId = "shed-1", rowVersion = 1),
                form = FormSpec.Empty,
                proofPolicy = ProofPolicy.Default,
            ),
            summaryOnRefresh = ShedCompletionSummaryDto(
                taskId = "task-1",
                expectedCount = 3,
                handledCount = 3,
                proofReadyCount = 3,
                proofMode = "per_goat_video",
                submitEnabled = true,
            ),
        )
        val vm = ScanViewModel(
            repo = doneRosterRepo(3),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = FakeScanCaptureRepository(),
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = tasks,
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals("shed-1", tasks.refreshedShedIds.single())
        assertTrue("server-confirmed shed proof readiness clears stale local orange state", vm.state.value.canSubmit)
        assertTrue(vm.state.value.proofActionNeeded.isEmpty())
    }

    @Test
    fun `server proof override still obeys backend submit enabled gate`() = runTest(dispatcher) {
        val tasks = FakeTasksRepositoryForCapture(
            detail = TaskDetail(
                task = TaskSummaryDto(taskId = "task-1", scopeType = "shed", scopeId = "shed-1", rowVersion = 1),
                form = FormSpec.Empty,
                proofPolicy = ProofPolicy.Default,
            ),
            initialSummary = ShedCompletionSummaryDto(
                taskId = "task-1",
                expectedCount = 3,
                handledCount = 3,
                proofReadyCount = 3,
                proofMode = "per_goat_video",
                submitEnabled = false,
            ),
        )
        val vm = ScanViewModel(
            repo = doneRosterRepo(3),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = FakeScanCaptureRepository(),
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = tasks,
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertFalse("local UI must not override a backend submit block with matching counts", vm.state.value.canSubmit)
        assertEquals(listOf("goat-1", "goat-2", "goat-3"), vm.state.value.proofActionNeeded.map { it.goatId })
    }

    @Test
    fun `task-wide persisted scans do not block current shed finalize`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        (1..3).forEach {
            scanCaptures.recordScan(
                taskId = "task-1",
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = "TAG-$it",
                goatId = "goat-$it",
                obligationId = "obl-$it",
                capturedAtMs = it.toLong(),
            )
        }
        val proofRepo = FakeProofCaptureRepository()
        seedSyncedProof(proofRepo, "goat-4")
        seedSyncedProof(proofRepo, "goat-5")
        val tasks = FakeTasksRepositoryForCapture(
            detail = TaskDetail(
                task = TaskSummaryDto(taskId = "task-1", scopeType = "shed", scopeId = "shed-2", rowVersion = 1),
                form = FormSpec.Empty,
                proofPolicy = ProofPolicy.Default,
            ),
            initialSummary = ShedCompletionSummaryDto(
                taskId = "task-1",
                expectedCount = 5,
                handledCount = 3,
                proofReadyCount = 2,
                proofMode = "per_goat_video",
                submitEnabled = false,
                blockingReason = "2 of 5 animals are not yet scanned.",
            ),
        )
        val vm = ScanViewModel(
            repo = rosterRepo(
                listOf(
                    scanRow("goat-4", "TAG-4", "obl-4").copy(status = "done"),
                    scanRow("goat-5", "TAG-5", "obl-5").copy(status = "done"),
                ),
            ),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = proofRepo,
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = tasks,
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-2", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(0, vm.state.value.pendingCount)
        assertEquals(listOf("goat-4", "goat-5"), vm.state.value.roster.map { it.goatId })
        assertTrue("Shed 2 finalize must ignore task-wide Shed 1 scans", vm.state.value.canSubmit)
        assertTrue(vm.state.value.proofActionNeeded.isEmpty())
    }

    @Test
    fun `shed-level proof policy does not show per-animal camera proof actions`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val vm = proofGateVm(
            doneRosterRepo(2),
            proofRepo,
            proofPolicy = ProofPolicy(
                proofMode = "shed_level_video",
                subjectScope = "shed",
                expectedSubjects = listOf("shed"),
                minimumCount = 1,
                maximumCount = 5,
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(0, vm.state.value.pendingCount)
        assertTrue("shed-level policy lets scan proceed to submit once animals are done", vm.state.value.canSubmit)
        assertTrue("shed-level proof is captured on submit screen, not per animal", vm.state.value.proofActionNeeded.isEmpty())
        assertTrue(vm.state.value.roster.all { !it.proofRequired })

        vm.onEvent(ScanEvent.CaptureProof("goat-1"))
        advanceUntilIdle()
        assertTrue("scan screen must not create goat proof captures for shed-level SOP", proofRepo.captureCalls.isEmpty())
    }

    @Test
    fun `a pending proof upload blocks submit and surfaces the animal`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val vm = proofGateVm(doneRosterRepo(3), proofRepo)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        seedSyncedProof(proofRepo, "goat-1")
        seedSyncedProof(proofRepo, "goat-2")
        proofRepo.capture(taskId = "task-1", fieldKey = "vaccination_goat_proof", subject = ProofSubject.GOAT, subjectId = "goat-3", localUri = "file://g3.mp4", mimeType = "video/mp4", caption = null, scopeType = "task", scopeId = "task-1", capturedStartMs = 1, capturedEndMs = 2, capturedByPrincipalId = "op") // stays PENDING
        advanceUntilIdle()

        assertFalse("a pending upload blocks submit", vm.state.value.canSubmit)
        assertEquals(listOf("goat-3"), vm.state.value.proofActionNeeded.map { it.goatId })
    }

    @Test
    fun `a failed proof upload blocks submit and marks the animal action-needed`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val vm = proofGateVm(doneRosterRepo(2), proofRepo)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        seedSyncedProof(proofRepo, "goat-1")
        val failing = proofRepo.capture(taskId = "task-1", fieldKey = "vaccination_goat_proof", subject = ProofSubject.GOAT, subjectId = "goat-2", localUri = "file://g2.mp4", mimeType = "video/mp4", caption = null, scopeType = "task", scopeId = "task-1", capturedStartMs = 1, capturedEndMs = 2, capturedByPrincipalId = "op") as AppResult.Ok
        proofRepo.markFailed(failing.value.id, "upload failed")
        advanceUntilIdle()

        assertFalse("a failed upload blocks submit", vm.state.value.canSubmit)
        val action = vm.state.value.proofActionNeeded.single()
        assertEquals("goat-2", action.goatId)
        assertEquals(sg.mesha.goatos.feature.scan.ProofUploadStatus.FAILED, action.proofUploadStatus)
    }

    @Test
    fun `a page-N done animal still requires proof before submit`() = runTest(dispatcher) {
        // 21 done animals → the 21st is on page two (below the 20-row window). Its missing proof must
        // still block submit and appear in the action-needed list even though it is off-screen.
        val proofRepo = FakeProofCaptureRepository()
        val vm = proofGateVm(doneRosterRepo(21), proofRepo)
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        assertEquals("window plus off-page done row is visible", 21, vm.state.value.roster.size)
        assertTrue(vm.state.value.hasMore)

        (1..20).forEach { seedSyncedProof(proofRepo, "goat-$it") } // everyone in the window is synced
        advanceUntilIdle()

        assertFalse("the off-window page-N animal still blocks submit", vm.state.value.canSubmit)
        assertEquals(listOf("goat-21"), vm.state.value.proofActionNeeded.map { it.goatId })
        assertTrue("goat-21 is restored into the visible done/proof context", vm.state.value.roster.any { it.goatId == "goat-21" })

        seedSyncedProof(proofRepo, "goat-21")
        advanceUntilIdle()
        assertTrue("all done animals synced ⇒ submit allowed", vm.state.value.canSubmit)
    }

    @Test
    fun `persisted page-N scan restores scanned goats feed after process recreation`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        scanCaptures.recordScan(
            taskId = "task-1",
            fieldKey = ROSTER_SCAN_FIELD_KEY,
            tag = "TAG-21",
            goatId = "goat-21",
            obligationId = "obl-21",
            capturedAtMs = 42L,
        )
        val vm = ScanViewModel(
            repo = pendingRosterRepo(21),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue("the bounded window still has more rows", vm.state.value.hasMore)
        assertEquals(1, vm.state.value.doneCount)
        assertTrue("persisted proof-missing scan below the first page must render in proof action rows", vm.state.value.proofActionNeeded.any { it.primaryTag == "TAG-21" })
        assertEquals(ScanStatus.DONE, vm.state.value.roster.single { it.goatId == "goat-21" }.status)
        assertEquals("scan screen re-entry must re-enqueue durable Room scans", 1, scanCaptures.enqueuePendingScansCalls)
    }

    private fun proofGateVm(
        repo: ExecutionRepository,
        proofRepo: FakeProofCaptureRepository,
        proofPolicy: ProofPolicy = ProofPolicy.Default,
    ): ScanViewModel =
        ScanViewModel(
            repo = repo,
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = FakeScanCaptureRepository(),
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = proofRepo,
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(
                TaskDetail(
                    task = TaskSummaryDto(taskId = "task-1", scopeType = "shed", scopeId = "shed-1", rowVersion = 1),
                    form = FormSpec.Empty,
                    proofPolicy = proofPolicy,
                ),
            ),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )

    private suspend fun seedSyncedProof(proofRepo: FakeProofCaptureRepository, goatId: String) {
        val created = proofRepo.capture(
            taskId = "task-1", fieldKey = "vaccination_goat_proof", subject = ProofSubject.GOAT,
            subjectId = goatId, localUri = "file://$goatId.mp4", mimeType = "video/mp4", caption = null,
            scopeType = "task", scopeId = "task-1", capturedStartMs = 1, capturedEndMs = 2,
            capturedByPrincipalId = "op",
        ) as AppResult.Ok
        proofRepo.markSynced(created.value.id, "server-$goatId")
    }

    private fun doneRosterRepo(n: Int): FakeScanExecutionRepository {
        val done = (1..n).map { scanRow("goat-$it", "TAG-$it", "obl-$it").copy(status = "done") }
        return rosterRepo(done)
    }

    private fun pendingRosterRepo(n: Int): FakeScanExecutionRepository {
        val pending = (1..n).map { scanRow("goat-$it", "TAG-$it", "obl-$it") }
        return rosterRepo(pending)
    }

    private fun rosterRepo(rows: List<ScanRosterRowDto>): FakeScanExecutionRepository {
        val pages = rows.chunked(20)
        check(pages.isNotEmpty())
        val continuation = mutableMapOf<String, ScanRosterResponseDto>()
        pages.forEachIndexed { index, pageRows ->
            val next = if (index + 1 < pages.size) "cursor-${index + 1}" else null
            val dto = ScanRosterResponseDto(rows = pageRows, nextCursor = next)
            if (index == 0) Unit else continuation["cursor-$index"] = dto
        }
        val first = ScanRosterResponseDto(rows = pages.first(), nextCursor = if (pages.size > 1) "cursor-1" else null)
        return FakeScanExecutionRepository(firstPage = first, continuationPages = continuation)
    }

}

private fun scanRow(goatId: String, tag: String, obligationId: String, secondaryTag: String? = null): ScanRosterRowDto =
    ScanRosterRowDto(
        goatId = goatId,
        primaryTag = tag,
        secondaryTag = secondaryTag,
        vaccineLabel = "ET",
        status = "pending",
        obligationId = obligationId,
    )

private class FakeRfidReaderPort : RfidReaderPort {
    private val readsFlow = MutableSharedFlow<RfidRead>()
    override val status: StateFlow<RfidReaderStatus> = MutableStateFlow(RfidReaderStatus.READY)
    override val reads: SharedFlow<RfidRead> = readsFlow
    override val readerName: StateFlow<String?> = MutableStateFlow("Test reader")
    override val devices: StateFlow<List<RfidReaderDevice>> = MutableStateFlow(emptyList())

    suspend fun emit(tag: String) {
        readsFlow.emit(RfidRead(tag = tag, capturedAtDeviceMs = 1L))
    }

    override fun refreshStatus() {}
    override fun openSystemPairing() {}
    override fun setCaptureEnabled(enabled: Boolean) {}
    override fun setCompletionKeySwallowEnabled(enabled: Boolean) {}
    override fun onKeyEvent(event: KeyEvent): Boolean = false
}

private class FakeScanExecutionRepository(
    private val firstPage: ScanRosterResponseDto,
    private val continuationPages: Map<String, ScanRosterResponseDto> = emptyMap(),
    warmCache: ScanRosterResponseDto? = null,
    private val refreshStarted: CompletableDeferred<Unit>? = null,
    private val refreshGate: CompletableDeferred<Unit>? = null,
) : ExecutionRepository {
    // In-memory per-row SSOT — the fake mirrors the production contract: refreshScanRoster walks the
    // WHOLE roster into this list (with backend seq order); every read is a bounded/aggregate query
    // over it. No whole-collection blob.
    private val rows = MutableStateFlow<List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity>>(emptyList())

    private fun norm(tag: String): String = tag.filter { it.isLetterOrDigit() }.lowercase()

    private fun ScanRosterRowDto.toEntity(shedId: String, taskId: String?, seq: Long) =
        sg.mesha.goatos.core.data.cache.ScanRosterRowEntity(
            id = "$shedId|${taskId ?: "shed-wide"}#${obligationId.ifBlank { "$goatId#$primaryTag" }}",
            scopeKey = "$shedId|${taskId ?: "shed-wide"}",
            shedId = shedId,
            taskId = taskId ?: "shed-wide",
            goatId = goatId,
            primaryTag = primaryTag,
            secondaryTag = secondaryTag,
            normalizedPrimaryTag = norm(primaryTag),
            normalizedSecondaryTag = secondaryTag?.let(::norm)?.takeIf { n -> n.isNotBlank() },
            vaccineLabel = vaccineLabel,
            status = status,
            obligationId = obligationId,
            seq = seq,
            updatedAt = 1L,
        )

    private fun statusIsDone(status: String): Boolean =
        status.lowercase().let { it.contains("done") || it.contains("complete") }

    init {
        warmCache?.let { page ->
            rows.value = page.rows.mapIndexed { index, row -> row.toEntity("shed-1", "task-1", index.toLong()) }
        }
    }

    override fun observeScanRosterRows(
        shedId: String,
        taskId: String?,
        windowSize: Int,
    ): Flow<List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity>> =
        rows.map { it.take(windowSize) }

    override fun observeScanRosterTotal(shedId: String, taskId: String?): Flow<Int> = rows.map { it.size }

    override fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?): Flow<List<String>> =
        rows.map { list -> list.filter { it.goatId.isNotBlank() && statusIsDone(it.status) }.map { it.goatId }.distinct() }

    override suspend fun scanRosterRowsByGoatIds(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
    ): List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity> =
        rows.value.filter { it.goatId in goatIds }

    override suspend fun findScanRosterByTag(
        shedId: String,
        taskId: String?,
        normalizedTag: String,
    ): sg.mesha.goatos.core.data.cache.ScanRosterRowEntity? =
        rows.value.firstOrNull {
            it.normalizedPrimaryTag == normalizedTag || it.normalizedSecondaryTag == normalizedTag
        }

    override fun observeScanRosterStatusCounts(
        shedId: String,
        taskId: String?,
    ): Flow<List<sg.mesha.goatos.core.data.cache.StatusCount>> =
        rows.map { list ->
            list.groupingBy { it.status }.eachCount()
                .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }
        }

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        taskId: String?,
        obligationIds: List<String>,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> =
        rows.value.filter { it.obligationId in obligationIds }
            .groupingBy { it.status }.eachCount()
            .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }

    override suspend fun getScanRosterStatusCounts(
        shedId: String,
        taskId: String?,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> =
        rows.value.groupingBy { it.status }.eachCount()
            .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?): Result<Unit> = runCatching {
        refreshStarted?.complete(Unit)
        refreshGate?.await()
        val staged = mutableListOf<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity>()
        var seq = 0L
        var page: ScanRosterResponseDto? = firstPage
        var cursor: String? = null
        val seen = mutableSetOf<String>()
        while (page != null) {
            page.rows.forEach { staged += it.toEntity(shedId, taskId, seq++) }
            cursor = page.nextCursor
            page = cursor?.let { c ->
                check(seen.add(c)) { "non-advancing cursor" }
                continuationPages[c] ?: error("missing page $c")
            }
        }
        rows.value = staged.distinctBy { it.id }
    }

    override suspend fun rows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        cursor: String?,
        includeFilterOptions: Boolean,
    ): VaccinationExecutionResponseDto = error("unused")

    override fun observeRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Flow<Resource<VaccinationExecutionResponseDto>> = error("unused")

    override suspend fun refreshRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = error("unused")

    override suspend fun appendRows(
        cursor: String,
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = error("unused")

    override suspend fun shed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?): VaccinationExecutionShedDrilldownDto =
        error("unused")

    override fun observeShed(
        shedId: String, asOf: String?, dueBefore: String?, limit: Int?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> = error("unused")

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?): Result<Unit> =
        error("unused")
}

private class FakeTaskRepository(
    private val task: TaskSummaryDto,
    private val form: FormSpec,
) : TasksRepository {
    private val taskDetail = MutableStateFlow(Resource<TaskDetail>(data = null))

    override suspend fun taskDetail(taskId: String): TaskDetail = TaskDetail(task = task, form = form)
    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> = taskDetail
    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = runCatching {
        taskDetail.value = Resource(data = TaskDetail(task = task, form = form), lastSyncedAt = 1L)
    }
    override fun observeShedCompletionSummary(taskId: String, shedId: String?): Flow<ShedCompletionSummaryDto?> =
        MutableStateFlow(null)
    override suspend fun refreshShedCompletionSummary(taskId: String, shedId: String?): Result<Unit> = Result.success(Unit)
}

private class CapturingSubmitSyncRepository : SyncRepository {
    var lastRequest: SubmitTaskRequestDto? = null
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = emptyFlow()

    override suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): AppResult<String> {
        lastRequest = request
        status.value = status.value.copy(
            items = listOf(
                SyncQueueItem(
                    id = "item-1",
                    opType = "shed_submit",
                    groupKey = groupKey,
                    status = SyncItemStatus.QUEUED,
                    attemptCount = 0,
                    maxAttempts = 5,
                    conflict = false,
                    createdAt = 1L,
                    updatedAt = 1L,
                    lastError = null,
                ),
            ),
        )
        return AppResult.Ok("item-1")
    }

    override suspend fun enqueueReschedule(
        obligationId: String,
        groupKey: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> =
        error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}


/**
 * Whether this person may capture vaccination proof is BACKEND-owned: the workforce bootstrap
 * compiles `vaccination_execute` from the caller's grants. It must never be re-derived on device
 * from `primary_role_hint`.
 *
 * Regression this locks: the old `primaryRoleHint == "operator"` literal locked out a pc_director
 * the backend HAD authorized (`vaccination_execute = true`). On the phone that read as -- the tag
 * scans, the animal flips to DONE, the proof camera never opens, and the row is stranded on
 * "Scan again to record proof", so the shed can never be submitted.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ScanViewModelExecutionGateTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    private fun viewModelFor(roleHint: String, flags: Map<String, Boolean>): ScanViewModel =
        ScanViewModel(
            repo = FakeScanExecutionRepository(
                firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
            ),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = FakeScanCaptureRepository(),
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(
                profile = BootstrapOperatorProfileDto(operatorId = "person-1", primaryRoleHint = roleHint),
                featureFlags = flags,
            ),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )

    @Test
    fun `director authorized by the backend may capture proof`() = runTest(dispatcher) {
        val vm = viewModelFor("pc_director", mapOf("vaccination_execute" to true))
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        // captureAccessRequired == "this person executes here, so grant camera/RFID access".
        assertTrue(vm.state.value.captureAccessRequired)
    }

    @Test
    fun `operator authorized by the backend may capture proof`() = runTest(dispatcher) {
        val vm = viewModelFor("operator", mapOf("vaccination_execute" to true))
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        assertTrue(vm.state.value.captureAccessRequired)
    }

    @Test
    fun `an operator role hint cannot grant capture the backend withheld`() = runTest(dispatcher) {
        val vm = viewModelFor("operator", mapOf("vaccination_execute" to false))
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        assertFalse(vm.state.value.captureAccessRequired)
    }

    @Test
    fun `a missing flag is treated as not authorized`() = runTest(dispatcher) {
        val vm = viewModelFor("verifier", emptyMap())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        assertFalse(vm.state.value.captureAccessRequired)
    }

}
