package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.FormField
import sg.mesha.goatos.core.data.forms.FormFieldType
import sg.mesha.goatos.core.data.forms.FormOption
import sg.mesha.goatos.core.data.forms.FormRule
import sg.mesha.goatos.core.data.forms.FormRuleType
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto
import sg.mesha.goatos.core.network.dto.VaccineBreakdownItemDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.rfid.FakeScanSource

/**
 * MOB-002 guardrails for the Submit shed-record screen's recording form.
 *
 * (`SubmitTaskRequestDto` sent with only `sopVersionId`/`idempotencyKey`, real answers never
 * captured, was the pre-MOB-002 bug): proves a required text/boolean recording-form field
 * blocks submit until answered, and that the ANSWERED value actually travels inside the
 * enqueued [SubmitTaskRequestDto.answers] — not silently dropped.
 *
 * MOB-002 capture: proves a required `goat_scan` field blocks submit until a tag is scanned
 * (via [FakeScanSource]) and the scanned tags travel in `answers`; proves a required
 * `video_proof` field blocks submit until a video is captured (via [FakeProofCaptureSource])
 * and the capture travels in `proofRefs` — Room-first, never a fabricated success.
 *
 * MOB-005 (loading/blocked/error states leaked `ScreenSamples` fixture farm identity — "Gandhi
 * 1", "Milking does" — onto a medical screen): proves the cold-cache/no-task/error states never
 * carry a shed name, cohort, date, or vaccine group.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SubmitViewModelFormTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(
        repository: TasksRepository,
        sync: SyncRepository,
        taskId: String?,
        scanCaptureRepository: FakeScanCaptureRepository = FakeScanCaptureRepository(),
        proofCaptureRepository: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        scanSource: FakeScanSource = FakeScanSource(),
        proofCaptureSource: FakeProofCaptureSource = FakeProofCaptureSource(),
        bootstrapRepository: FakeCaptureBootstrapRepository = FakeCaptureBootstrapRepository(),
        shedId: String? = null,
        sopVersionId: String? = null,
    ): SubmitViewModel = SubmitViewModel(
        repo = repository,
        syncRepository = sync,
        scanCaptureRepository = scanCaptureRepository,
        proofCaptureRepository = proofCaptureRepository,
        scanSource = scanSource,
        proofCaptureSource = proofCaptureSource,
        bootstrapRepository = bootstrapRepository,
        analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
        crashReporter = sg.mesha.goatos.core.analytics.NoopCrashReporter(),
        savedStateHandle = SavedStateHandle(
            buildMap {
                if (taskId != null) put("taskId", taskId)
                if (shedId != null) put("shedId", shedId)
                if (sopVersionId != null) put("sopVersionId", sopVersionId)
            },
        ),
    )

    @Test
    fun `ConfirmSubmit without the gate having been raised submits nothing`() = runTest(dispatcher) {
        // The gate is a pause in FRONT of submit()'s validations, never a way around them.
        // confirmSubmit() returns early unless submit() actually raised it, so a stray
        // ConfirmSubmit -- a double tap, a replayed event, a restored dialog -- must not push a
        // shed past a pending proof or a read-only task. This drives the real ViewModel and
        // fails if that early return is removed; the assertion it replaced built a
        // SubmitUiState(showSubmitConfirmation = true) by hand and asserted the value it had
        // just passed in, which held for the broken build too.
        val task = TaskSummaryDto(taskId = "task-gate", sopVersionId = "sop-1", scopeId = "shed-1", title = "Gandhi 1", rowVersion = 1)
        val form = FormSpec(schemaVersion = "goatos.sop-form.v1", fields = emptyList(), rules = emptyList())
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(FakeFormTasksRepository(task, form), sync, "task-gate")
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertNull("confirming a gate that was never raised must enqueue nothing", sync.lastRequest)
        assertFalse("and must not leave the dialog flag raised", viewModel.state.value.showSubmitConfirmation)
    }

    @Test
    fun `submit is blocked until a required form field is answered, then real answers travel`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-1", sopVersionId = "sop-1", scopeId = "shed-1", title = "Gandhi 1", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "cold_chain_verified", label = "Cold chain verified", type = FormFieldType.BOOLEAN, required = true),
                FormField(key = "dose_ml_given", label = "Dose (ml)", type = FormFieldType.TEXT, required = true),
            ),
            rules = emptyList(),
        )
        val repository = FakeFormTasksRepository(task, form)
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(repository, sync, "task-1")
        // MOB-010: the Room task-detail/scan/proof observers are now gated on `state` having a
        // subscriber — keep it hot for the duration of the test the same way a real screen would.
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()
        assertFalse("submit must stay blocked while a required field is unanswered", viewModel.state.value.canSubmit)

        viewModel.onEvent(SubmitEvent.FormToggle("cold_chain_verified", true))
        advanceUntilIdle()
        assertFalse("one required field answered is not enough", viewModel.state.value.canSubmit)

        viewModel.onEvent(SubmitEvent.FormText("dose_ml_given", "2"))
        advanceUntilIdle()
        assertTrue("submit must unblock once every required field is answered", viewModel.state.value.canSubmit)
        assertNull(viewModel.state.value.formRunner?.blockedReason)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        val request = sync.lastRequest
        assertEquals("real captured answers must travel in the submit payload", JsonPrimitive(true), request?.answers?.get("cold_chain_verified"))
        assertEquals(JsonPrimitive("2"), request?.answers?.get("dose_ml_given"))
    }

    @Test
    fun `vaccination select date time and explicit false answers obey backend rules and travel`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-contract", sopVersionId = "sop-contract", scopeId = "shed-1", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField("cold_chain_verified", "Cold chain verified", FormFieldType.BOOLEAN, required = true),
                FormField(
                    "route_site",
                    "Route / site",
                    FormFieldType.SELECT,
                    required = true,
                    options = listOf(FormOption("subcutaneous", "Subcutaneous")),
                ),
                FormField("administered_at", "Administered at", FormFieldType.DATE_TIME, required = true),
                FormField("adverse_reaction", "Adverse reaction observed", FormFieldType.BOOLEAN, required = true),
                FormField("adverse_reaction_notes", "Adverse reaction notes", FormFieldType.TEXT),
            ),
            rules = listOf(
                FormRule(
                    type = FormRuleType.BLOCK_SUBMISSION_IF,
                    conditionField = "cold_chain_verified",
                    operator = "equals",
                    value = JsonPrimitive(false),
                    message = "Cold chain must be verified before submitting the drive.",
                ),
                FormRule(
                    type = FormRuleType.REQUIRED_IF,
                    field = "adverse_reaction_notes",
                    conditionField = "adverse_reaction",
                    operator = "equals",
                    value = JsonPrimitive(true),
                    message = "Adverse reaction notes are required when a reaction is observed.",
                ),
            ),
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(FakeFormTasksRepository(task, form), sync, "task-contract")
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertFalse(
            "conditional reaction notes must stay out of the form until a reaction is observed",
            viewModel.state.value.formRunner?.fields?.any { it.key == "adverse_reaction_notes" } == true,
        )

        viewModel.onEvent(SubmitEvent.FormToggle("cold_chain_verified", false))
        advanceUntilIdle()
        assertEquals("Cold chain must be verified before submitting the drive.", viewModel.state.value.formRunner?.blockedReason)

        viewModel.onEvent(SubmitEvent.FormToggle("cold_chain_verified", true))
        viewModel.onEvent(SubmitEvent.FormPick("route_site", "subcutaneous"))
        viewModel.onEvent(SubmitEvent.FormText("administered_at", "2026-07-20T04:45:00+05:30"))
        viewModel.onEvent(SubmitEvent.FormToggle("adverse_reaction", true))
        advanceUntilIdle()
        assertFalse(viewModel.state.value.canSubmit)
        assertTrue(viewModel.state.value.formRunner?.fields?.first { it.key == "adverse_reaction_notes" }?.required == true)

        viewModel.onEvent(SubmitEvent.FormToggle("adverse_reaction", false))
        advanceUntilIdle()
        assertTrue("explicit No is a complete required boolean answer", viewModel.state.value.canSubmit)
        assertFalse(
            "reaction notes must collapse again when the operator explicitly answers No",
            viewModel.state.value.formRunner?.fields?.any { it.key == "adverse_reaction_notes" } == true,
        )

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertEquals(JsonPrimitive("subcutaneous"), sync.lastRequest?.answers?.get("route_site"))
        assertEquals(JsonPrimitive("2026-07-20T04:45:00+05:30"), sync.lastRequest?.answers?.get("administered_at"))
        assertEquals(JsonPrimitive(false), sync.lastRequest?.answers?.get("adverse_reaction"))
    }

    @Test
    fun `a required goat_scan field blocks submit until a tag is scanned, then tags travel`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-scan", sopVersionId = "sop-scan", scopeId = "shed-3", title = "Scan shed", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField(key = "goat_scan", label = "Scan goats", type = FormFieldType.GOAT_SCAN, required = true)),
            rules = emptyList(),
        )
        val repository = FakeFormTasksRepository(task, form)
        val sync = CapturingSyncRepository()
        val scanSource = FakeScanSource()
        val viewModel = viewModel(repository, sync, "task-scan", scanSource = scanSource)
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()
        assertFalse(viewModel.state.value.canSubmit)
        assertEquals(0, viewModel.state.value.formRunner?.fields?.first()?.scannedCount)

        viewModel.onEvent(SubmitEvent.ScanToggled("goat_scan"))
        advanceUntilIdle()
        assertEquals(1, scanSource.startCount)
        assertTrue(viewModel.state.value.formRunner?.fields?.first()?.scanning == true)

        scanSource.emit("TAG-100")
        scanSource.emit("TAG-100") // repeat — dedup must keep the count at 1
        scanSource.emit("TAG-101")
        advanceUntilIdle()

        assertEquals(2, viewModel.state.value.formRunner?.fields?.first()?.scannedCount)
        assertTrue(viewModel.state.value.canSubmit)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        val answer = sync.lastRequest?.answers?.get("goat_scan") as? JsonArray
        assertEquals(listOf(JsonPrimitive("TAG-100"), JsonPrimitive("TAG-101")), answer)
        // Submitting stops the active scan — hardware capture is never left running behind
        // a screen that just enqueued its write.
        assertEquals(1, scanSource.stopCount)
    }

    @Test
    fun `resolved vaccination scans submit canonical goat ids that match proof subjects`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-goat-id", sopVersionId = "sop-goat-id", scopeId = "shed-3", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("goat_ids", "Goats", FormFieldType.GOAT_SCAN, required = true)),
            rules = emptyList(),
        )
        val scans = FakeScanCaptureRepository()
        scans.recordScan(
            taskId = task.taskId,
            fieldKey = "goat_ids",
            tag = "RFID-001",
            goatId = "goat-uuid-1",
            obligationId = "obligation-1",
        )
        val proofs = FakeProofCaptureRepository()
        val proof = proofs.capture(
            taskId = task.taskId,
            fieldKey = "vaccination_goat_proof",
            subject = ProofSubject.GOAT,
            subjectId = "goat-uuid-1",
            localUri = "file:///proof.mp4",
            mimeType = "video/mp4",
            caption = null,
            scopeType = "task",
            scopeId = task.taskId,
            capturedStartMs = 1L,
            capturedEndMs = 2L,
            capturedByPrincipalId = "operator-1",
            proofPolicy = ProofPolicy.Default,
        ) as AppResult.Ok
        proofs.markSynced(proof.value.id, "server-proof-1")
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            repository = FakeFormTasksRepository(task, form),
            sync = sync,
            taskId = task.taskId,
            scanCaptureRepository = scans,
            proofCaptureRepository = proofs,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertTrue(viewModel.state.value.canSubmit)
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertEquals(
            JsonArray(listOf(JsonPrimitive("goat-uuid-1"))),
            sync.lastRequest?.answers?.get("goat_ids"),
        )
    }

    @Test
    fun `a required video_proof field blocks submit until uploaded, then server proof ref travels`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-2", sopVersionId = "sop-2", scopeId = "shed-2", title = "Sumathi 1", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "administration_video", label = "Administration video", type = FormFieldType.VIDEO_PROOF, required = true),
            ),
            rules = emptyList(),
        )
        val repository = FakeFormTasksRepository(task, form)
        val sync = CapturingSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            repository,
            sync,
            "task-2",
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()
        assertFalse(viewModel.state.value.canSubmit)
        assertEquals(
            "Record the required video (\"Administration video\") before submitting.",
            viewModel.state.value.formRunner?.blockedReason,
        )

        // Cancelled capture (operator backs out) — must NOT unblock or fake a success.
        proofCaptureSource.queue(null)
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("administration_video"))
        advanceUntilIdle()
        assertFalse(viewModel.state.value.canSubmit)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://admin.mp4", startedAtMs = 1_000L, endedAtMs = 4_500L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("administration_video"))
        advanceUntilIdle()

        assertFalse("local capture alone is not enough; the proof must finish signed upload + completion", viewModel.state.value.canSubmit)
        assertEquals(
            "Wait for proof upload to finish before submitting.",
            viewModel.state.value.formRunner?.blockedReason,
        )
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNull("submit must not enqueue a payload with a local Room proof id", sync.lastRequest)

        val localProofId = viewModel.state.value.formRunner?.fields?.single()?.proofItems?.single()?.id
        proofCaptureRepository.markSynced(localProofId!!, "server-proof-administration")
        advanceUntilIdle()

        assertTrue("submit must unblock once the captured proof has a completed backend proof id", viewModel.state.value.canSubmit)
        assertNull(viewModel.state.value.formRunner?.blockedReason)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        val request = sync.lastRequest
        assertEquals(1, request?.proofRefs?.size)
        assertEquals("server-proof-administration", request?.proofRefs?.first()?.proofId)
        assertEquals("administration", request?.proofRefs?.first()?.subjectType)
        assertEquals("completed", request?.proofRefs?.first()?.uploadState)
        assertTrue("nothing enqueues twice for a cancelled + successful capture", proofCaptureSource.captureCount == 2)
    }

    @Test
    fun `a failed optional video_proof does not block an otherwise complete form`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-optional-proof", sopVersionId = "sop-optional-proof", scopeId = "shed-9", title = "Optional proof", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "cold_chain_verified", label = "Cold chain verified", type = FormFieldType.BOOLEAN, required = true),
                FormField(key = "extra_video", label = "Extra video", type = FormFieldType.VIDEO_PROOF, required = false),
            ),
            rules = emptyList(),
        )
        val repository = FakeFormTasksRepository(task, form)
        val sync = CapturingSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            repository,
            sync,
            "task-optional-proof",
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()
        viewModel.onEvent(SubmitEvent.FormToggle("cold_chain_verified", true))
        advanceUntilIdle()
        assertTrue(viewModel.state.value.canSubmit)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://optional.mp4", startedAtMs = 2_000L, endedAtMs = 5_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("extra_video"))
        advanceUntilIdle()
        val optionalProofId = viewModel.state.value.formRunner?.fields?.single { it.key == "extra_video" }?.proofItems?.single()?.id
        proofCaptureRepository.markFailed(optionalProofId!!, "network gave up")
        advanceUntilIdle()

        assertTrue("failed optional proof should not wedge submit", viewModel.state.value.canSubmit)
        assertNull(viewModel.state.value.formRunner?.blockedReason)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertEquals(0, sync.lastRequest?.proofRefs?.size)
        assertEquals(JsonPrimitive(true), sync.lastRequest?.answers?.get("cold_chain_verified"))
    }

    @Test
    fun `a pending optional video_proof blocks submit so it is not silently orphaned`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-optional-pending-proof", sopVersionId = "sop-optional-pending-proof", scopeId = "shed-9", title = "Optional proof", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "cold_chain_verified", label = "Cold chain verified", type = FormFieldType.BOOLEAN, required = true),
                FormField(key = "extra_video", label = "Extra video", type = FormFieldType.VIDEO_PROOF, required = false),
            ),
            rules = emptyList(),
        )
        val sync = CapturingSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form),
            sync,
            "task-optional-pending-proof",
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()
        viewModel.onEvent(SubmitEvent.FormToggle("cold_chain_verified", true))
        advanceUntilIdle()
        assertTrue(viewModel.state.value.canSubmit)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://optional-pending.mp4", startedAtMs = 2_000L, endedAtMs = 5_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("extra_video"))
        advanceUntilIdle()

        assertFalse("pending optional proof must wait instead of being dropped from proof_refs", viewModel.state.value.canSubmit)
        assertEquals("Wait for proof upload to finish before submitting.", viewModel.state.value.formRunner?.blockedReason)
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNull(sync.lastRequest)

        val proofId = viewModel.state.value.formRunner?.fields?.single { it.key == "extra_video" }?.proofItems?.single()?.id
        proofCaptureRepository.markSynced(proofId!!, "server-proof-extra")
        advanceUntilIdle()

        assertTrue(viewModel.state.value.canSubmit)
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertEquals("server-proof-extra", sync.lastRequest?.proofRefs?.single()?.proofId)
    }

    @Test
    fun `a second pending proof on an already answered required field blocks submit`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-second-proof", sopVersionId = "sop-second-proof", scopeId = "shed-2", title = "Sumathi 1", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "administration_video", label = "Administration video", type = FormFieldType.VIDEO_PROOF, required = true),
            ),
            rules = emptyList(),
        )
        val sync = CapturingSyncRepository()
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form),
            sync,
            "task-second-proof",
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        proofCaptureSource.queue(CapturedVideo(localUri = "file://admin-1.mp4", startedAtMs = 1_000L, endedAtMs = 4_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("administration_video"))
        advanceUntilIdle()
        val firstProofId = viewModel.state.value.formRunner?.fields?.single()?.proofItems?.single()?.id
        proofCaptureRepository.markSynced(firstProofId!!, "server-proof-admin-1")
        advanceUntilIdle()
        assertTrue(viewModel.state.value.canSubmit)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://admin-2.mp4", startedAtMs = 5_000L, endedAtMs = 8_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("administration_video"))
        advanceUntilIdle()

        assertFalse("second pending proof must wait instead of being dropped", viewModel.state.value.canSubmit)
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNull(sync.lastRequest)
    }

    @Test
    fun `a failed required video_proof can be re-recorded instead of exhausting the field`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-required-failed-proof", sopVersionId = "sop-required-failed-proof", scopeId = "shed-2", title = "Required proof", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "administration_video", label = "Administration video", type = FormFieldType.VIDEO_PROOF, required = true),
            ),
            rules = emptyList(),
        )
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form),
            CapturingSyncRepository(),
            "task-required-failed-proof",
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        proofCaptureSource.queue(CapturedVideo(localUri = "file://admin-failed.mp4", startedAtMs = 1_000L, endedAtMs = 4_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("administration_video"))
        advanceUntilIdle()
        val failedProofId = viewModel.state.value.formRunner?.fields?.single()?.proofItems?.single()?.id
        proofCaptureRepository.markFailed(failedProofId!!, "network gave up")
        advanceUntilIdle()

        val field = viewModel.state.value.formRunner?.fields?.single()
        assertFalse(viewModel.state.value.canSubmit)
        assertTrue("terminal failed proof must not consume the only capture slot", field?.canCaptureMore == true)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://admin-retry.mp4", startedAtMs = 5_000L, endedAtMs = 8_500L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("administration_video"))
        advanceUntilIdle()

        assertEquals(2, viewModel.state.value.formRunner?.fields?.single()?.proofItems?.size)
        assertEquals(2, proofCaptureSource.captureCount)
    }

    @Test
    fun `a failed proof row exposes a retry action wired to the proof upload repository`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-required-retry-proof", sopVersionId = "sop-required-retry-proof", scopeId = "shed-2", title = "Required proof", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "administration_video", label = "Administration video", type = FormFieldType.VIDEO_PROOF, required = true),
            ),
            rules = emptyList(),
        )
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form),
            CapturingSyncRepository(),
            "task-required-retry-proof",
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        proofCaptureSource.queue(CapturedVideo(localUri = "file://admin-failed.mp4", startedAtMs = 1_000L, endedAtMs = 4_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("administration_video"))
        advanceUntilIdle()
        val failedProofId = viewModel.state.value.formRunner?.fields?.single()?.proofItems?.single()?.id
        proofCaptureRepository.markFailed(failedProofId!!, "network gave up")
        advanceUntilIdle()

        assertTrue(viewModel.state.value.formRunner?.fields?.single()?.proofItems?.single()?.retryable == true)

        viewModel.onEvent(SubmitEvent.ProofRetryRequested("administration_video", failedProofId))
        advanceUntilIdle()

        val retried = viewModel.state.value.formRunner?.fields?.single()?.proofItems?.single()
        assertEquals("PENDING", retried?.syncStatus)
        assertFalse(retried?.retryable == true)
    }

    @Test
    fun `five failed proof rows do not block replacement capture at the ViewModel layer`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-five-failed-proof", sopVersionId = "sop-five-failed-proof", scopeId = "shed-2", title = "Required proof", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "administration_video", label = "Administration video", type = FormFieldType.VIDEO_PROOF, required = true, repeat = true),
            ),
            rules = emptyList(),
        )
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form),
            CapturingSyncRepository(),
            "task-five-failed-proof",
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        repeat(5) { index ->
            val captured = proofCaptureRepository.capture(
                taskId = "task-five-failed-proof",
                fieldKey = "administration_video",
                subject = ProofSubject.ADMINISTRATION,
                localUri = "file://failed-$index.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "task",
                scopeId = "task-five-failed-proof",
                capturedStartMs = index * 1_000L,
                capturedEndMs = index * 1_000L + 500L,
                capturedByPrincipalId = "operator-1",
            ) as AppResult.Ok
            proofCaptureRepository.markFailed(captured.value.id, "network gave up")
        }
        advanceUntilIdle()

        assertTrue(viewModel.state.value.formRunner?.fields?.single()?.canCaptureMore == true)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://replacement.mp4", startedAtMs = 10_000L, endedAtMs = 13_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("administration_video"))
        advanceUntilIdle()

        assertEquals(1, proofCaptureSource.captureCount)
        assertEquals("file://replacement.mp4", proofCaptureRepository.captureCalls.last().localUri)
        assertEquals(6, viewModel.state.value.formRunner?.fields?.single()?.proofItems?.size)
    }

    @Test
    fun `cold cache, no-task-assigned, and task-load-failed states never render fixture farm identity`() = runTest(dispatcher) {
        // Cold cache: task id present, Room + network both never answer (never call refreshTaskDetail
        // successfully) — exercised via a repository whose Flow never emits real data.
        val stuckRepository = object : TasksRepository {
            override suspend fun taskDetail(taskId: String): TaskDetail = error("unused")
            override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> =
                MutableStateFlow(Resource(data = null))
            override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = Result.failure(IllegalStateException("offline"))
            override fun observeShedCompletionSummary(taskId: String, shedId: String?): Flow<ShedCompletionSummaryDto?> =
                MutableStateFlow(null)
            override suspend fun refreshShedCompletionSummary(taskId: String, shedId: String?): Result<Unit> =
                Result.failure(IllegalStateException("offline"))
        }
        val coldCacheVm = viewModel(stuckRepository, CapturingSyncRepository(), "task-x")
        backgroundScope.launch { coldCacheVm.state.collect {} }
        advanceUntilIdle()
        assertFixtureFree(coldCacheVm.state.value)
        assertTrue(coldCacheVm.state.value.isTaskLoadFailed)

        // No task assigned: taskId is blank.
        val noTaskVm = viewModel(stuckRepository, CapturingSyncRepository(), null)
        backgroundScope.launch { noTaskVm.state.collect {} }
        advanceUntilIdle()
        assertFixtureFree(noTaskVm.state.value)
        assertTrue(noTaskVm.state.value.isNoTaskAssigned)
    }

    @Test
    fun `vaccination record never exposes an internal task UUID as shed identity`() = runTest(dispatcher) {
        val rawId = "00000000-0000-4000-8000-000000003001"
        val task = TaskSummaryDto(
            taskId = "task-safe-presentation",
            sopVersionId = "sop-safe-presentation",
            taskType = "vaccination",
            scopeType = "park",
            scopeId = rawId,
            title = "Vaccination drive $rawId",
            description = "",
            rowVersion = 1,
        )
        val viewModel = viewModel(
            FakeFormTasksRepository(task, FormSpec.Empty),
            CapturingSyncRepository(),
            "task-safe-presentation",
        )
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()

        val state = viewModel.state.value
        assertFalse("raw UUID leaked into the user-visible shed summary", state.shed.contains(rawId))
        assertFalse("raw UUID leaked into the user-visible title", state.title.contains(rawId))
    }

    /**
     * A SHED-scoped round must never be acknowledged without evidence that THIS round was sent.
     *
     * Live failure, twice. The operator rescanned an animal a verifier had sent back, recorded the
     * proof and tapped Finalize. The screen showed "Shed record submitted · Synced · record on
     * file" while the database held NO submission, NO completion and NO verification item -- the
     * task carried `needs_review` from the EARLIER round, that reads as "submission terminal", and
     * the ack was inferred from the ABSENCE of a negative. Weighing is worse: its shed-completion
     * summary endpoint does not exist, so the guarded branch never runs at all and every weighing
     * submit took this path.
     */
    @Test
    fun `a shed-scoped round is not acknowledged without evidence this round was sent`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-round",
            sopVersionId = "sop-shed-round",
            // Terminal-sounding state left behind by the PREVIOUS round.
            state = "needs_review",
            rowVersion = 2,
            scopeType = "shed",
            scopeId = "shed-godel-1",
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(key = "cold_chain_verified", label = "Cold chain verified", type = FormFieldType.BOOLEAN, required = true),
            ),
            rules = emptyList(),
        )
        val viewModel = viewModel(FakeFormTasksRepository(task, form), CapturingSyncRepository(), task.taskId)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertNotEquals(
            "a shed round with no submission evidence must never render as acknowledged",
            sg.mesha.goatos.feature.submit.SyncState.ACKED,
            viewModel.state.value.syncState,
        )
    }

    @Test
    fun `a submitted task is acknowledged read only and cannot be submitted again`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-needs-review",
            sopVersionId = "sop-needs-review",
            state = "needs_review",
            rowVersion = 2,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(
                    key = "cold_chain_verified",
                    label = "Cold chain verified",
                    type = FormFieldType.BOOLEAN,
                    required = true,
                ),
            ),
            rules = emptyList(),
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(FakeFormTasksRepository(task, form), sync, task.taskId)
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()

        assertEquals(sg.mesha.goatos.feature.submit.SyncState.ACKED, viewModel.state.value.syncState)
        assertFalse(viewModel.state.value.canSubmit)
        assertNull("terminal task fields must not remain editable", viewModel.state.value.formRunner)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNull("terminal task must never enqueue another submission", sync.lastRequest)
    }

    @Test
    fun `vaccination shed completion renders a read-only summary and gates Submit on backend submit_enabled`() = runTest(dispatcher) {
        // The vaccination SOP form_dsl has no manual medical fields — shed completion is an
        // acknowledgement. The screen must render the backend shed-completion summary (human shed
        // name, drive name, counts, vaccine breakdown) and enable Submit ONLY when the backend
        // says submit_enabled == true.
        val task = TaskSummaryDto(
            taskId = "task-shed-ack",
            sopVersionId = "sop-shed-ack",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-A",
            rowVersion = 1,
        )
        val blockedSummary = ShedCompletionSummaryDto(
            taskId = "task-shed-ack",
            shedName = "Shed A — Weaners",
            driveName = "Vaccination · July 2026",
            expectedCount = 50,
            handledCount = 47,
            proofReadyCount = 45,
            vaccineBreakdown = listOf(
                VaccineBreakdownItemDto(vaccine = "FMD", count = 47),
                VaccineBreakdownItemDto(vaccine = "PPR", count = 45),
            ),
            submitEnabled = false,
            blockingReason = "3 animals not yet scanned",
            submitState = "draft",
        )
        // Empty SOP form_dsl (no manual fields) — the summary stands in for the form.
        val repository = FakeFormTasksRepository(task, FormSpec.Empty, shedSummary = blockedSummary)
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(repository, sync, "task-shed-ack")
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val blocked = viewModel.state.value
        assertEquals("Shed A — Weaners", blocked.shedCompletionSummary?.shedName)
        assertEquals("Vaccination · July 2026", blocked.shedCompletionSummary?.driveName)
        assertEquals(50, blocked.shedCompletionSummary?.expectedCount)
        assertEquals(47, blocked.shedCompletionSummary?.handledCount)
        assertEquals(45, blocked.shedCompletionSummary?.proofReadyCount)
        assertEquals(2, blocked.vaccineBreakdown.size)
        assertEquals("FMD", blocked.vaccineBreakdown.first().vaccine)
        assertNull("no empty manual form is rendered for the vaccination shed ack", blocked.formRunner)
        assertFalse("Submit stays disabled while backend submit_enabled is false", blocked.canSubmit)
        assertEquals("3 animals not yet scanned", blocked.blockingReason)

        // Submit while blocked must never reach the outbox.
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNull("a backend-blocked shed ack must never enqueue a submission", sync.lastRequest)
    }

    @Test
    fun `vaccination shed completion enables Submit when backend reports it ready`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-ready",
            sopVersionId = "sop-shed-ready",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-B",
            rowVersion = 1,
        )
        val readySummary = ShedCompletionSummaryDto(
            taskId = "task-shed-ready",
            shedName = "Shed B — Adults",
            driveName = "Vaccination · July 2026",
            expectedCount = 30,
            handledCount = 30,
            proofReadyCount = 30,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "FMD", count = 30)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "draft",
        )
        val repository = FakeFormTasksRepository(task, FormSpec.Empty, shedSummary = readySummary)
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(repository, sync, "task-shed-ready")
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val ready = viewModel.state.value
        assertTrue("Submit enables once backend submit_enabled is true", ready.canSubmit)
        assertNull("no blocking reason when submit is enabled", ready.blockingReason)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNotNull("a ready shed ack must enqueue the acknowledgement submission", sync.lastRequest)
    }

    @Test
    fun `per-goat proof summary submit does not get blocked by hidden SOP form`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-per-goat-proof-ready",
            sopVersionId = "sop-per-goat-proof-ready",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-proof",
            rowVersion = 1,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField("goat_scan", "Scan goats", FormFieldType.GOAT_SCAN, required = true),
                FormField("goat_video", "Goat video", FormFieldType.VIDEO_PROOF, required = true),
            ),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "per_goat_video",
            subjectScope = "goat",
            expectedSubjects = listOf("goat"),
            minimumCount = 1,
            maximumCount = 1,
            allowedCaptureSources = listOf("in_app_camera"),
        )
        val readySummary = ShedCompletionSummaryDto(
            taskId = task.taskId,
            shedName = "Shed proof",
            driveName = "Vaccination · July 2026",
            expectedCount = 3,
            handledCount = 3,
            proofReadyCount = 3,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 3)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "draft",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = readySummary),
            sync,
            task.taskId,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertTrue("per-goat summary readiness should enable Submit", viewModel.state.value.canSubmit)
        assertNull("per-goat proof mode hides the generic SOP form gate", viewModel.state.value.formRunner)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertNotNull("tapping Submit must enqueue the shed acknowledgement", sync.lastRequest)
    }

    @Test
    fun `submit_enabled true overrides a stale terminal submit_state after a per-goat rework rescan`() = runTest(dispatcher) {
        // Regression for the P0 where a shed with an EARLIER accepted/verified round (task.state
        // stays "accepted" at the whole-task level) has 3 goats rejected at the item level,
        // rescanned, and re-proofed. The backend's readiness gate (submit_enabled/blocking_reason)
        // is scoped to the CURRENT round and correctly reports ready-to-submit, but submit_state
        // still carries the terminal-sounding word "verified" from the earlier round. The client
        // must trust submit_enabled, not the coarse submit_state word, and must never render the
        // "Shed record submitted" acknowledgement screen over work that was never sent.
        val task = TaskSummaryDto(
            taskId = "task-per-goat-rework-verified",
            sopVersionId = "sop-per-goat-rework-verified",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-gandhi-1",
            state = "accepted",
            rowVersion = 4,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField("goat_scan", "Scan goats", FormFieldType.GOAT_SCAN, required = true),
                FormField("goat_video", "Goat video", FormFieldType.VIDEO_PROOF, required = true),
            ),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "per_goat_video",
            subjectScope = "goat",
            expectedSubjects = listOf("goat"),
            minimumCount = 1,
            maximumCount = 1,
            allowedCaptureSources = listOf("in_app_camera"),
        )
        val reworkReadySummary = ShedCompletionSummaryDto(
            taskId = task.taskId,
            shedName = "Gandhi 1",
            driveName = "Per-animal vaccination proof QA",
            expectedCount = 3,
            handledCount = 3,
            proofReadyCount = 3,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 3)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "verified",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = reworkReadySummary),
            sync,
            task.taskId,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals(
            "submit_enabled=true must never render the terminal ack screen",
            sg.mesha.goatos.feature.submit.SyncState.DRAFT,
            viewModel.state.value.syncState,
        )
        assertTrue("submit_enabled=true must leave the submit control enabled", viewModel.state.value.canSubmit)
        assertNull("no blocking reason when submit is enabled", viewModel.state.value.blockingReason)

        viewModel.onEvent(SubmitEvent.Submit)
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertNotNull("Finalize must actually enqueue the shed acknowledgement", sync.lastRequest)
    }

    @Test
    fun `shed-level SOP proof captures shed subject from camera or gallery and enforces five-video cap`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-proof",
            sopVersionId = "sop-shed-proof",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-C",
            title = "Shed C submit",
            rowVersion = 1,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(
                FormField(
                    key = "shed_video",
                    label = "Shed vaccination video",
                    type = FormFieldType.VIDEO_PROOF,
                    required = true,
                    repeat = true,
                ),
            ),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            maximumCountPerSubject = 1,
            captureSource = "in_app_camera",
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy),
            CapturingSyncRepository(),
            task.taskId,
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val initialField = viewModel.state.value.formRunner?.fields?.single()
        assertEquals("Shed vaccination video", initialField?.label)
        assertTrue("shed-level SOP should expose gallery picker", initialField?.allowGalleryPicker == true)
        assertEquals("Add at least 1 shed video before submitting.", viewModel.state.value.formRunner?.blockedReason)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://camera.mp4", startedAtMs = 1_000L, endedAtMs = 4_000L, captureSource = "in_app_camera"))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("shed_video"))
        advanceUntilIdle()

        proofCaptureSource.queue(CapturedVideo(localUri = "file://gallery.mp4", startedAtMs = 5_000L, endedAtMs = 8_000L, captureSource = "gallery_picker"))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("shed_video", source = "gallery_picker"))
        advanceUntilIdle()

        assertEquals(2, proofCaptureRepository.captureCalls.size)
        proofCaptureRepository.captureCalls.forEach { call ->
            assertEquals(ProofSubject.SHED, call.subject)
            assertEquals("shed-C", call.subjectId)
        }
        assertEquals("file://gallery.mp4", proofCaptureRepository.captureCalls.last().localUri)

        proofCaptureRepository.captureCalls.takeLast(2).forEachIndexed { index, _ ->
            val localId = viewModel.state.value.formRunner?.fields?.single()?.proofItems?.get(index)?.id
            proofCaptureRepository.markSynced(localId!!, "server-shed-proof-${index + 1}")
        }
        advanceUntilIdle()
        assertTrue("one synced shed video is enough for required shed proof", viewModel.state.value.canSubmit)

        repeat(3) { index ->
            proofCaptureSource.queue(CapturedVideo(localUri = "file://extra-$index.mp4", startedAtMs = 10_000L + index, endedAtMs = 11_000L + index))
            viewModel.onEvent(SubmitEvent.CaptureVideoRequested("shed_video"))
            advanceUntilIdle()
        }
        assertEquals(5, proofCaptureRepository.captureCalls.size)
        assertFalse("five active shed-level videos hit the SOP cap", viewModel.state.value.formRunner?.fields?.single()?.canCaptureMore == true)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://sixth.mp4", startedAtMs = 20_000L, endedAtMs = 21_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("shed_video"))
        advanceUntilIdle()
        assertEquals("sixth shed video must be rejected by SOP cap", 5, proofCaptureRepository.captureCalls.size)
    }

    @Test
    fun `backend-ready shed-level proof satisfies submit after local Room proof cache is empty`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-server-proof",
            sopVersionId = "sop-shed-server-proof",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-D",
            rowVersion = 1,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("shed_video", "Shed vaccination video", FormFieldType.VIDEO_PROOF, required = true)),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val readySummary = ShedCompletionSummaryDto(
            taskId = "task-shed-server-proof",
            shedName = "Shed D",
            driveName = "Vaccination · July 2026",
            expectedCount = 6,
            handledCount = 6,
            proofReadyCount = 1,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 6)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "draft",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = readySummary),
            sync,
            task.taskId,
            proofCaptureRepository = FakeProofCaptureRepository(),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val field = viewModel.state.value.formRunner?.fields?.single()
        assertTrue("backend proofReadyCount should render a server proof item after reinstall", field?.proofCaptured == true)
        assertEquals("SYNCED", field?.proofItems?.single()?.syncStatus)
        assertTrue("backend-ready shed proof should satisfy the required video field", viewModel.state.value.canSubmit)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNotNull(sync.lastRequest)
    }

    @Test
    fun `park-level terminal task does not mark an unsubmitted shed as acknowledged`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-parent-needs-review",
            sopVersionId = "sop-shed-parent-needs-review",
            taskType = "vaccination",
            scopeType = "park",
            scopeId = "park-1",
            state = "needs_review",
            rowVersion = 2,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("shed_video", "Shed vaccination video", FormFieldType.VIDEO_PROOF, required = true)),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val unsubmittedShedSummary = ShedCompletionSummaryDto(
            taskId = "task-shed-parent-needs-review",
            shedName = "Godel 1",
            driveName = "Vaccination · July 2026",
            expectedCount = 120,
            handledCount = 120,
            proofReadyCount = 1,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 120)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "draft",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = unsubmittedShedSummary),
            sync,
            task.taskId,
            proofCaptureRepository = FakeProofCaptureRepository(),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals(sg.mesha.goatos.feature.submit.SyncState.DRAFT, viewModel.state.value.syncState)
        assertTrue("uploaded shed proof should make the unsubmitted shed ready to submit", viewModel.state.value.canSubmit)
        assertEquals("1 of 5 shed videos synced · 1 required", viewModel.state.value.proofSummarySyncedLabel)

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNotNull("unsubmitted shed must still enqueue the final submission", sync.lastRequest)
    }

    @Test
    fun `park-scoped shed route submits with active shed idempotency scope`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shared-parent",
            sopVersionId = "sop-shared-parent",
            taskType = "vaccination",
            scopeType = "park",
            scopeId = "park-parent",
            rowVersion = 4,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("shed_video", "Shed vaccination video", FormFieldType.VIDEO_PROOF, required = true)),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val readySummary = ShedCompletionSummaryDto(
            taskId = "task-shared-parent",
            shedName = "Godel 2",
            driveName = "Vaccination · July 2026",
            expectedCount = 10,
            handledCount = 10,
            proofReadyCount = 1,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 10)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "draft",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = readySummary),
            sync,
            task.taskId,
            proofCaptureRepository = FakeProofCaptureRepository(),
            shedId = "godel-2",
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertEquals("godel-2", sync.lastGroupKey)
        assertEquals("shed-submit:task-shared-parent:scope:godel-2:rv:4", sync.lastIdempotencyKey)
        assertEquals("shed-submit:task-shared-parent:scope:godel-2:rv:4", sync.lastRequest?.idempotencyKey)
    }

    @Test
    fun `submit uses route sop version when cached task detail omits it`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-route-version",
            sopVersionId = "",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-route-version",
            rowVersion = 1,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("shed_video", "Shed vaccination video", FormFieldType.VIDEO_PROOF, required = true)),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val readySummary = ShedCompletionSummaryDto(
            taskId = task.taskId,
            shedName = "Godel 2",
            driveName = "Vaccination",
            expectedCount = 1,
            handledCount = 1,
            proofReadyCount = 1,
            submitEnabled = true,
            submitState = "draft",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = readySummary),
            sync,
            task.taskId,
            shedId = "shed-route-version",
            sopVersionId = "b0000000-0000-4000-8000-000000000002",
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertEquals("b0000000-0000-4000-8000-000000000002", sync.lastRequest?.sopVersionId)
    }

    @Test
    fun `terminal shed task keeps proof form visible while an optional shed video is uploading`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-terminal-uploading-proof",
            sopVersionId = "sop-shed-terminal-uploading-proof",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-uploading",
            state = "needs_review",
            rowVersion = 2,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("shed_video", "Shed vaccination video", FormFieldType.VIDEO_PROOF, required = true)),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val submittedSummary = ShedCompletionSummaryDto(
            taskId = "task-shed-terminal-uploading-proof",
            shedName = "Shed Uploading",
            driveName = "Vaccination · July 2026",
            expectedCount = 2,
            handledCount = 2,
            proofReadyCount = 1,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 2)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "needs_review",
        )
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = submittedSummary),
            CapturingSyncRepository(),
            task.taskId,
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals(sg.mesha.goatos.feature.submit.SyncState.ACKED, viewModel.state.value.syncState)
        assertEquals("Shed proof videos", viewModel.state.value.proofSummaryTitle)
        assertEquals("1 of 5 shed videos synced · 1 required", viewModel.state.value.proofSummarySyncedLabel)
        assertEquals(1, viewModel.state.value.shedCompletionSummary?.proofReadyCount)

        proofCaptureSource.queue(CapturedVideo(localUri = "file://shed-uploading-extra.mp4", startedAtMs = 2_000L, endedAtMs = 5_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("shed_video"))
        advanceUntilIdle()

        assertEquals(sg.mesha.goatos.feature.submit.SyncState.DRAFT, viewModel.state.value.syncState)
        assertNotNull("active shed video upload must keep the proof upload form visible", viewModel.state.value.formRunner)
        assertEquals(1, viewModel.state.value.proofUploading)
    }

    @Test
    fun `verified shed completion renders acknowledged state`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-verified-proof",
            sopVersionId = "sop-shed-verified-proof",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-verified",
            state = "accepted",
            rowVersion = 3,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("shed_video", "Shed vaccination video", FormFieldType.VIDEO_PROOF, required = true)),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val verifiedSummary = ShedCompletionSummaryDto(
            taskId = "task-shed-verified-proof",
            shedName = "Verified Shed",
            driveName = "Vaccination · July 2026",
            expectedCount = 2,
            handledCount = 2,
            proofReadyCount = 1,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 2)),
            submitEnabled = false,
            blockingReason = null,
            submitState = "verified",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = verifiedSummary),
            sync,
            task.taskId,
            proofCaptureRepository = FakeProofCaptureRepository(),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals(sg.mesha.goatos.feature.submit.SyncState.ACKED, viewModel.state.value.syncState)
        assertNull("verified shed record must not reopen the final submit form", viewModel.state.value.formRunner)
        assertFalse(viewModel.state.value.canSubmit)
        assertNull(sync.lastRequest)
    }

    @Test
    fun `successful outbox submit immediately renders acknowledged state before task refresh catches up`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-outbox-acked",
            sopVersionId = "sop-shed-outbox-acked",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-outbox",
            state = "draft",
            rowVersion = 1,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("shed_video", "Shed vaccination video", FormFieldType.VIDEO_PROOF, required = true)),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val readySummary = ShedCompletionSummaryDto(
            taskId = "task-shed-outbox-acked",
            shedName = "Shed Outbox",
            driveName = "Vaccination · July 2026",
            expectedCount = 2,
            handledCount = 2,
            proofReadyCount = 1,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 2)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "draft",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = readySummary),
            sync,
            task.taskId,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertTrue(viewModel.state.value.canSubmit)
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        sync.markSucceeded()
        advanceUntilIdle()

        assertEquals(sg.mesha.goatos.feature.submit.SyncState.ACKED, viewModel.state.value.syncState)
        assertFalse(viewModel.state.value.canSubmit)
        assertNull("accepted outbox submit must not repaint the editable proof form", viewModel.state.value.formRunner)
    }

    @Test
    fun `cached terminal per goat shed task reopens submit when shed summary is draft`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-per-goat-stale-terminal",
            sopVersionId = "sop-per-goat-stale-terminal",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-stale",
            state = "needs_review",
            rowVersion = 2,
        )
        val policy = ProofPolicy(
            proofMode = "per_goat_video",
            subjectScope = "goat",
            expectedSubjects = listOf("goat"),
            minimumCount = 1,
            maximumCount = 1,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val draftSummary = ShedCompletionSummaryDto(
            taskId = "task-per-goat-stale-terminal",
            shedName = "Shed Stale",
            driveName = "Vaccination · July 2026",
            expectedCount = 3,
            handledCount = 3,
            proofReadyCount = 3,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 3)),
            submitEnabled = true,
            blockingReason = null,
            submitState = "draft",
        )
        val sync = CapturingSyncRepository()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, FormSpec.Empty, proofPolicy = policy, shedSummary = draftSummary),
            sync,
            task.taskId,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertEquals(sg.mesha.goatos.feature.submit.SyncState.DRAFT, viewModel.state.value.syncState)
        assertTrue("draft shed summary must reopen the submit action even if task cache is terminal", viewModel.state.value.canSubmit)
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertNotNull(sync.lastRequest)
    }

    @Test
    fun `local synced shed proof updates completion summary before backend refresh catches up`() = runTest(dispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-shed-local-proof",
            sopVersionId = "sop-shed-local-proof",
            taskType = "vaccination",
            scopeType = "shed",
            scopeId = "shed-E",
            rowVersion = 1,
        )
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField("shed_video", "Shed vaccination video", FormFieldType.VIDEO_PROOF, required = true)),
            rules = emptyList(),
        )
        val policy = ProofPolicy(
            proofMode = "shed_level_video",
            subjectScope = "shed",
            expectedSubjects = listOf("shed"),
            minimumCount = 1,
            maximumCount = 5,
            allowedCaptureSources = listOf("in_app_camera", "gallery_picker"),
        )
        val staleSummary = ShedCompletionSummaryDto(
            taskId = "task-shed-local-proof",
            shedName = "Shed E",
            driveName = "Vaccination · July 2026",
            expectedCount = 2,
            handledCount = 2,
            proofReadyCount = 0,
            vaccineBreakdown = listOf(VaccineBreakdownItemDto(vaccine = "ET+TT", count = 2)),
            submitEnabled = false,
            blockingReason = "Add at least 1 shed video before submitting.",
            submitState = "draft",
        )
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val viewModel = viewModel(
            FakeFormTasksRepository(task, form, proofPolicy = policy, shedSummary = staleSummary),
            CapturingSyncRepository(),
            task.taskId,
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        proofCaptureSource.queue(CapturedVideo(localUri = "file://shed-e.mp4", startedAtMs = 1_000L, endedAtMs = 4_000L))
        viewModel.onEvent(SubmitEvent.CaptureVideoRequested("shed_video"))
        advanceUntilIdle()
        val localId = viewModel.state.value.formRunner?.fields?.single()?.proofItems?.single()?.id
        proofCaptureRepository.markSynced(localId!!, "server-shed-proof-e")
        advanceUntilIdle()

        assertEquals(1, viewModel.state.value.shedCompletionSummary?.proofReadyCount)
        assertEquals("1 of 5 shed videos synced · 1 required", viewModel.state.value.proofSummarySyncedLabel)
        assertTrue(viewModel.state.value.canSubmit)
    }

    @Test
    fun `an approver-only principal with no operator profile is capture-role-blocked`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-role", sopVersionId = "sop-role", scopeId = "shed-4", title = "Role shed", rowVersion = 1)
        val form = FormSpec(
            schemaVersion = "goatos.sop-form.v1",
            fields = listOf(FormField(key = "cold_chain_verified", label = "Cold chain verified", type = FormFieldType.BOOLEAN, required = true)),
            rules = emptyList(),
        )
        val repository = FakeFormTasksRepository(task, form)
        val viewModel = viewModel(
            repository,
            CapturingSyncRepository(),
            "task-role",
            // Capture authority is backend-owned now: an approver is blocked because the backend
            // withholds `vaccination_execute`, NOT because the client noticed a missing profile.
            bootstrapRepository = FakeCaptureBootstrapRepository(profile = null, featureFlags = emptyMap()),
        )
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()
        assertTrue(viewModel.state.value.isCaptureRoleBlocked)
        assertFalse(viewModel.state.value.canSubmit)

        viewModel.onEvent(SubmitEvent.FormToggle("cold_chain_verified", true))
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()
        assertFalse("a role-blocked principal must never reach the outbox", viewModel.state.value.canSubmit)
    }

    private fun assertFixtureFree(state: sg.mesha.goatos.feature.submit.SubmitUiState) {
        assertEquals("", state.shed)
        assertEquals("", state.cohort)
        assertEquals("", state.date)
        assertTrue(state.groups.isEmpty())
        assertFalse(state.canSubmit)
    }
}

private class FakeFormTasksRepository(
    private val task: TaskSummaryDto,
    private val form: FormSpec,
    private val proofPolicy: ProofPolicy = ProofPolicy(expectedSubjects = emptyList()), // R50-027: fall back to per-key mapping when expectedSubjects is empty
    private val shedSummary: ShedCompletionSummaryDto? = null,
) : TasksRepository {
    private val flow = MutableStateFlow(Resource<TaskDetail>(data = null))
    private val summaryFlow = MutableStateFlow<ShedCompletionSummaryDto?>(null)

    override suspend fun taskDetail(taskId: String): TaskDetail = TaskDetail(task = task, form = form, proofPolicy = proofPolicy)

    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> = flow

    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = runCatching {
        flow.value = Resource(data = TaskDetail(task = task, form = form, proofPolicy = proofPolicy), lastSyncedAt = 1L)
    }

    override fun observeShedCompletionSummary(taskId: String, shedId: String?): Flow<ShedCompletionSummaryDto?> = summaryFlow

    override suspend fun refreshShedCompletionSummary(taskId: String, shedId: String?): Result<Unit> = runCatching {
        summaryFlow.value = shedSummary
    }
}

private class CapturingSyncRepository : SyncRepository {
    var lastRequest: SubmitTaskRequestDto? = null
    var lastGroupKey: String? = null
    var lastIdempotencyKey: String? = null
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val item = MutableStateFlow<SyncQueueItem?>(null)

    override fun observeStatus(): StateFlow<SyncStatus> = status

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = item

    override suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): AppResult<String> {
        lastRequest = request
        lastGroupKey = groupKey
        lastIdempotencyKey = idempotencyKey
        val queued = SyncQueueItem(
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
        )
        item.value = queued
        status.value = status.value.copy(
            items = listOf(queued),
        )
        return AppResult.Ok("item-1")
    }

    fun markSucceeded() {
        val current = item.value ?: return
        item.value = current.copy(status = SyncItemStatus.SUCCEEDED, updatedAt = current.updatedAt + 1)
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

    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")

    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)

    override suspend fun triggerDrain() = Unit
}
