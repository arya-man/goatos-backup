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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            crashReporter = sg.mesha.goatos.core.analytics.NoopCrashReporter(),
        )
        backgroundScope.launch { submitVm.state.collect {} }
        advanceUntilIdle()

        assertTrue("scan-screen rows must satisfy the required GOAT_SCAN field", submitVm.state.value.canSubmit)
        submitVm.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        submitVm.onEvent(SubmitEvent.ConfirmSubmit)
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
        val repo = FakeScanExecutionRepository(
            firstPage = ScanRosterResponseDto(rows = listOf(scanRow("goat-1", "TAG-100", "obl-1"))),
        )
        val vm = ScanViewModel(
            repo = repo,
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    "shedId" to "shed-1",
                    "taskId" to "task-1",
                    "scanTitle" to "Gandhi 1 - Part 3",
                    "partitionLabel" to "Part 3",
                ),
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals("Gandhi 1 - Part 3 Scan", vm.state.value.cohortLabel)
        assertEquals("Part 3", repo.lastRefreshPartitionLabel)
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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

    /**
     * CONFIRMED SHED DEFECT (maintainer, real shed use): "When I'm on camera recording and I use
     * the RFID reader to scan, it suddenly stops recording and comes out." The old behaviour
     * cancelled whatever camera session was still open the moment a DIFFERENT goat's tag was
     * read, destroying an unrecoverable in-progress recording. This is the invariant that
     * replaces it: a scan for a different goat while a capture is in flight must NEVER stop or
     * cancel that capture. The new goat is queued instead and its camera opens automatically once
     * the in-flight capture's job completes.
     */
    @Test
    fun `scanning a second goat while the first goat's proof video is still recording must NOT stop or cancel the first camera -- it queues the second goat instead`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
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
            analytics = analytics,
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        // Goat A is scanned; its camera opens and the recording is still in progress (suspended
        // on the gate, i.e. captureVideo() has NOT returned) when the operator walks to goat B and
        // scans it.
        val goatAGate = proofSource.queueGate()
        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals("goat A's camera opened", 1, proofSource.captureCount)
        assertEquals(0, proofRepo.captureCalls.size)

        reader.emit("TAG-200")
        advanceUntilIdle()

        // THE INVARIANT: goat A's still-recording camera session is untouched -- no second camera
        // opens, nothing is stopped or cancelled. Goat B is queued, and the operator is told so
        // instead of the queued scan silently vanishing.
        assertEquals("a scan mid-recording must never open a second camera or disturb the first", 1, proofSource.captureCount)
        assertEquals(0, proofRepo.captureCalls.size)
        assertEquals("TAG-200 queued — camera opens once TAG-100's video is saved.", scanVm.state.value.duplicateNotice)
        assertTrue(
            "a deferred scan must be recorded in analytics, not silently swallowed",
            analytics.names().contains(sg.mesha.goatos.core.analytics.AnalyticsEvents.PROOF_CAPTURE_SCAN_DEFERRED),
        )

        // Goat A's recording finishes normally and saves under A's own subject id -- the in-flight
        // capture was never touched by B's scan.
        goatAGate.complete(CapturedVideo(localUri = "file://goat-a.mp4", startedAtMs = 1, endedAtMs = 2))
        advanceUntilIdle()
        assertEquals(1, proofRepo.captureCalls.size)
        assertEquals("goat-1", proofRepo.captureCalls[0].subjectId)
        assertEquals("file://goat-a.mp4", proofRepo.captureCalls[0].localUri)

        // The queued goat B's camera opens automatically the instant A's camera frees up -- the
        // operator never has to remember to rescan B.
        assertEquals("goat B's camera must open automatically once A's capture completed", 2, proofSource.captureCount)
    }

    /**
     * Production shares ONE buffered result channel across every capture request
     * (`VideoCaptureLauncher.kt` / `ProofCaptureRelay`), so this exercises the real plumbing
     * rather than [FakeProofCaptureSource]'s per-call isolation. Because a scan for a different
     * goat no longer cancels the in-flight capture, goat B's camera never opens while A's is still
     * recording, so there is no way for A's eventually-finalized clip to be misdelivered to B's
     * request -- the wrong-goat-attribution hazard the relay was built for is now structurally
     * unreachable via this path (queuing serialises captures one goat at a time).
     */
    @Test
    fun `a scan for a different goat never stops the in-flight recording, and its own finalized clip still lands under its own subject id`() = runTest(dispatcher) {
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        // Goat A is scanned; the camera opens. The operator presses stop, but CameraX finalization
        // is asynchronous -- no result has been delivered yet.
        reader.emit("TAG-100")
        advanceUntilIdle()
        assertEquals("goat A's camera opened", 1, proofSource.captureCount)

        // The operator turns to goat B and scans it. THE INVARIANT: this must not stop A's camera --
        // no second request is opened; B is queued instead.
        reader.emit("TAG-200")
        advanceUntilIdle()
        assertEquals("goat B's scan must not open a second camera while A is still recording", 1, proofSource.captureCount)

        // A's recording finalizes for request #1 -- the only request that exists -- and lands under
        // A's own subject id.
        proofSource.deliverRecorderResult(
            requestOrdinal = 1,
            video = CapturedVideo(localUri = "file://goat-a.mp4", startedAtMs = 1, endedAtMs = 2),
        )
        advanceUntilIdle()
        assertEquals(1, proofRepo.captureCalls.size)
        assertEquals("goat-1", proofRepo.captureCalls.single().subjectId)
        assertEquals("file://goat-a.mp4", proofRepo.captureCalls.single().localUri)

        // B's camera now opens automatically (queued goat), and its own recording lands under B's
        // own subject id.
        assertEquals("goat B's camera opens once A's capture is done", 2, proofSource.captureCount)
        proofSource.deliverRecorderResult(
            requestOrdinal = 2,
            video = CapturedVideo(localUri = "file://goat-b.mp4", startedAtMs = 3, endedAtMs = 4),
        )
        advanceUntilIdle()
        assertEquals(2, proofRepo.captureCalls.size)
        assertEquals("goat-2", proofRepo.captureCalls[1].subjectId)
        assertEquals("file://goat-b.mp4", proofRepo.captureCalls[1].localUri)
    }

    /**
     * Only the LAST queued goat survives: if a third goat is scanned before the queued second one
     * ever got its turn, the second is overtaken and its camera will never open. That is a genuine
     * drop (the operator scanned it but it will never be acted on) and must be visible and
     * recorded, never silent -- this is the one case where a scan really is dropped, as distinct
     * from merely deferred.
     */
    @Test
    fun `only the last queued goat survives a further scan, and the overtaken one is surfaced as a visible, analytics-tracked drop`() = runTest(dispatcher) {
        val proofRepo = FakeProofCaptureRepository()
        val proofSource = FakeProofCaptureSource()
        val reader = FakeRfidReaderPort()
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
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
            analytics = analytics,
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        val goatAGate = proofSource.queueGate()
        reader.emit("TAG-100") // camera opens for goat A, recording
        advanceUntilIdle()
        assertEquals(1, proofSource.captureCount)

        reader.emit("TAG-200") // goat B queued behind A
        advanceUntilIdle()
        reader.emit("TAG-300") // goat C overtakes goat B in the queue -- B is dropped
        advanceUntilIdle()

        assertEquals("neither queued scan may open a camera while A is still recording", 1, proofSource.captureCount)
        assertTrue(
            "goat B being overtaken by goat C must be recorded as a drop",
            analytics.names().contains(sg.mesha.goatos.core.analytics.AnalyticsEvents.PROOF_CAPTURE_SCAN_DROPPED),
        )
        assertEquals("TAG-200 still needs its video — dropped for TAG-300.", scanVm.state.value.duplicateNotice)

        // A's recording finishes; the camera that opens next must be goat C's (the surviving
        // queued goat), never goat B's (the overtaken one).
        goatAGate.complete(CapturedVideo(localUri = "file://goat-a.mp4", startedAtMs = 1, endedAtMs = 2))
        advanceUntilIdle()
        assertEquals(2, proofSource.captureCount)
        assertEquals(1, proofRepo.captureCalls.size)
        assertEquals("goat-1", proofRepo.captureCalls[0].subjectId)
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
    fun `nothing completes a row without a physical RFID read`() = runTest(dispatcher) {
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        // R50-024 narrowed a manual ring tap to "draft-only" but LEFT THE AFFORDANCE. The
        // maintainer then hit it in the field: tapping the progress ring completed the very animal
        // a verifier had just REJECTED and re-enabled "Finalize shed" on a shed still holding an
        // unvaccinated goat. There is now NO manual completion path at all -- nothing on this
        // screen may advance a row without a physical tag read.
        assertEquals(
            "no untouched roster row may read as done before a tag is read",
            ScanStatus.PENDING,
            scanVm.state.value.roster.single().status,
        )
        assertEquals(emptyList<String>(), scanCaptures.tagsForTask("task-1"))
        assertTrue(scanAttempts.calls.isEmpty())

        reader.emit("TAG-100")
        advanceUntilIdle()

        // The physical reader hit is the ONLY thing that completes the row, and it writes durable
        // evidence: exactly one accepted attempt and one roster-scan capture.
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            crashReporter = sg.mesha.goatos.core.analytics.NoopCrashReporter(),
        )
        backgroundScope.launch { submitVm.state.collect {} }
        advanceUntilIdle()

        assertTrue("optional scan fields should not block submission", submitVm.state.value.canSubmit)
        submitVm.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        submitVm.onEvent(SubmitEvent.ConfirmSubmit)
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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

    private fun rosterRepo(
        rows: List<ScanRosterRowDto>,
        rosterUpdatedAtMs: Long = 10_000L,
    ): FakeScanExecutionRepository {
        val pages = rows.chunked(20)
        check(pages.isNotEmpty())
        val continuation = mutableMapOf<String, ScanRosterResponseDto>()
        pages.forEachIndexed { index, pageRows ->
            val next = if (index + 1 < pages.size) "cursor-${index + 1}" else null
            val dto = ScanRosterResponseDto(rows = pageRows, nextCursor = next)
            if (index == 0) Unit else continuation["cursor-$index"] = dto
        }
        val first = ScanRosterResponseDto(rows = pages.first(), nextCursor = if (pages.size > 1) "cursor-1" else null)
        return FakeScanExecutionRepository(
            firstPage = first,
            continuationPages = continuation,
            rosterUpdatedAtMs = rosterUpdatedAtMs,
        )
    }

    /**
     * PARITY after a verifier REJECTION: the scan screen must not keep an animal green once the
     * backend reopened its obligation.
     *
     * Live repro on Godel 1. The verifier rejected goat 1001's proof, the backend reopened
     * obligation 3001 to `due`, and the sheds list, the shed drilldown AND the scan roster all
     * reported "3 targeted / 1 open / 2 done". The scan screen alone showed 3/3 green and offered
     * "Finalize shed", because a rejected animal keeps its scannedAt forever and the row status was
     * derived from that timestamp instead of the server status.
     *
     * scannedAt is deliberately NON-NULL here: an earlier version of this test left it null, so it
     * passed without ever exercising the timestamp path while the real device stayed wrong.
     *
     * Pairs with the process-recreation tests above, which pin the OPPOSITE case: an unsynced
     * capture must stay green offline. Only a capture the backend has already seen may be overruled.
     */
    /**
     * The OPPOSITE invariant, and the one the reconciliation could most easily break: an UNSYNCED
     * capture must keep showing DONE even while the server still reports the obligation open.
     *
     * That is not staleness, it is ordinary offline scanning -- the operator scanned in a shed with
     * no signal, the backend has not seen the capture yet, and of course it still says `due`. An
     * earlier attempt at this fix dropped those too and broke seven tests; only a capture the
     * backend has ALREADY SEEN (SYNCED) may be overruled by it.
     *
     * Deliberately the same fixture as the rejection test above, changing ONLY the sync status, so
     * the two cases are read side by side and neither can be "fixed" without failing the other.
     */
    @Test
    fun `an unsynced capture keeps showing done while the server has not seen it`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        scanCaptures.recordScan(
            taskId = "task-1",
            fieldKey = ROSTER_SCAN_FIELD_KEY,
            tag = "901007000504418",
            goatId = "goat-1",
            obligationId = "obl-1",
            capturedAtMs = 1L,
        )
        // NOT marked synced: the outbox has not drained, so the backend cannot know about it.

        val vm = ScanViewModel(
            repo = rosterRepo(
                listOf(
                    scanRow("goat-1", "901007000504418", "obl-1").copy(status = "due"),
                    scanRow("goat-2", "901007000504332", "obl-2").copy(status = "done"),
                    scanRow("goat-3", "901007000504407", "obl-3").copy(status = "done"),
                ),
            ),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(
            "an offline scan the backend has not seen must stay DONE",
            ScanStatus.DONE,
            vm.state.value.roster.first { it.obligationId == "obl-1" }.status,
        )
    }

    /**
     * The race a judge caught in the first version of this fix.
     *
     * `syncStatus` flips to SYNCED the instant the capture reaches the server, but the roster cache
     * only reloads on screen entry / pull-to-refresh / navigate-back. In that window the cached row
     * still says `pending` from BEFORE the scan -- and the first implementation read that as "the
     * server says this is still open" and pulled the tick off a freshly scanned, never rejected
     * animal, in front of the operator, recoverable only by a manual refresh.
     *
     * A roster row OLDER than the capture cannot have an opinion about it yet. Here the row was
     * fetched at t=1 and the scan taken at t=5_000, so the local evidence must stand.
     */
    @Test
    fun `a stale roster row older than the capture cannot un-complete a fresh scan`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        scanCaptures.recordScan(
            taskId = "task-1",
            fieldKey = ROSTER_SCAN_FIELD_KEY,
            tag = "901007000504418",
            goatId = "goat-1",
            obligationId = "obl-1",
            capturedAtMs = 5_000L,
        )
        // The outbox drained: the backend has the capture. The roster has NOT been refetched since.
        scanCaptures.markLocalScanSynced("task-1", ROSTER_SCAN_FIELD_KEY, "901007000504418")

        val vm = ScanViewModel(
            repo = rosterRepo(
                listOf(scanRow("goat-1", "901007000504418", "obl-1").copy(status = "pending")),
                // Roster page fetched BEFORE the scan was taken -- it predates the capture.
                rosterUpdatedAtMs = 1L,
            ),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(
            "a roster row older than the capture must not un-complete it",
            ScanStatus.DONE,
            vm.state.value.roster.first { it.obligationId == "obl-1" }.status,
        )
    }

    @Test
    fun `a synced capture yields to a reopened obligation after rejection`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        scanCaptures.recordScan(
            taskId = "task-1",
            fieldKey = ROSTER_SCAN_FIELD_KEY,
            tag = "901007000504418",
            goatId = "goat-1",
            obligationId = "obl-1",
            capturedAtMs = 1L,
        )
        // The backend accepted this capture; the verifier then rejected the proof.
        scanCaptures.markLocalScanSynced("task-1", ROSTER_SCAN_FIELD_KEY, "901007000504418")

        val vm = ScanViewModel(
            repo = rosterRepo(
                listOf(
                    scanRow("goat-1", "901007000504418", "obl-1")
                        .copy(status = "due", scannedAt = "2026-08-06T03:40:00Z"),
                    scanRow("goat-2", "901007000504332", "obl-2").copy(status = "done"),
                    scanRow("goat-3", "901007000504407", "obl-3").copy(status = "done"),
                ),
            ),
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = scanCaptures,
            scanAttemptRepository = FakeScanAttemptRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            tasksRepository = FakeTasksRepositoryForCapture(),
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(
            "a reopened obligation must not read as DONE",
            ScanStatus.PENDING,
            state.roster.first { it.obligationId == "obl-1" }.status,
        )
        assertEquals("the ring must match the server's done count", 2, state.doneCount)
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
    private val rosterUpdatedAtMs: Long = 10_000L,
    warmCache: ScanRosterResponseDto? = null,
    private val refreshStarted: CompletableDeferred<Unit>? = null,
    private val refreshGate: CompletableDeferred<Unit>? = null,
) : ExecutionRepository {
    // In-memory per-row SSOT — the fake mirrors the production contract: refreshScanRoster walks the
    // WHOLE roster into this list (with backend seq order); every read is a bounded/aggregate query
    // over it. No whole-collection blob.
    private val rows = MutableStateFlow<List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity>>(emptyList())
    var lastRefreshPartitionLabel: String? = null
        private set

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
            // When the roster page was FETCHED. Defaults to "just now" (a real fetch stamps the
            // clock); tests that model a STALE cache pass an older value than the capture's time.
            updatedAt = rosterUpdatedAtMs,
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
        partitionLabel: String?,
    ): Flow<List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity>> =
        rows.map { it.take(windowSize) }

    override fun observeScanRosterTotal(shedId: String, taskId: String?, partitionLabel: String?): Flow<Int> = rows.map { it.size }

    override fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<String>> =
        rows.map { list -> list.filter { it.goatId.isNotBlank() && statusIsDone(it.status) }.map { it.goatId }.distinct() }

    override suspend fun scanRosterRowsByGoatIds(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String?,
    ): List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity> =
        rows.value.filter { it.goatId in goatIds }

    override suspend fun findScanRosterByTag(
        shedId: String,
        taskId: String?,
        normalizedTag: String,
        partitionLabel: String?,
    ): sg.mesha.goatos.core.data.cache.ScanRosterRowEntity? =
        rows.value.firstOrNull {
            it.normalizedPrimaryTag == normalizedTag || it.normalizedSecondaryTag == normalizedTag
        }

    override fun observeScanRosterStatusCounts(
        shedId: String,
        taskId: String?,
        partitionLabel: String?,
    ): Flow<List<sg.mesha.goatos.core.data.cache.StatusCount>> =
        rows.map { list ->
            list.groupingBy { it.status }.eachCount()
                .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }
        }

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        taskId: String?,
        obligationIds: List<String>,
        partitionLabel: String?,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> =
        rows.value.filter { it.obligationId in obligationIds }
            .groupingBy { it.status }.eachCount()
            .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }

    override suspend fun getScanRosterStatusCounts(
        shedId: String,
        taskId: String?,
        partitionLabel: String?,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> =
        rows.value.groupingBy { it.status }.eachCount()
            .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?, partitionLabel: String?): Result<Unit> = runCatching {
        lastRefreshPartitionLabel = partitionLabel
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

    override suspend fun shed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): VaccinationExecutionShedDrilldownDto =
        error("unused")

    override fun observeShed(
        shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> = error("unused")

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): Result<Unit> =
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
                    idempotencyKey = "test-idempotency-key",
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
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
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
