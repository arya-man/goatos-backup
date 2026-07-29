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
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.core.analytics.NoopAnalytics
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

        assertEquals("restored evidence must not write another roster capture", 1, scanCaptures.recordScanCalls)
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
        assertEquals("duplicate scans are a notice, not another visible feed row", 1, scanVm.state.value.feed.size)
        assertEquals("Already scanned · ET", scanVm.state.value.duplicateNotice)
        assertEquals(ScanStatus.DONE, scanVm.state.value.roster.single().status)
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
    fun `rescan of proof missing goat opens proof path without recording another roster capture`() = runTest(dispatcher) {
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
        assertEquals("proof rescan must not create a second roster capture", 1, scanCaptures.recordScanCalls)
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
