package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
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
import sg.mesha.goatos.core.data.forms.FormField
import sg.mesha.goatos.core.data.forms.FormFieldType
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
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
    ): SubmitViewModel = SubmitViewModel(
        repo = repository,
        syncRepository = sync,
        scanCaptureRepository = scanCaptureRepository,
        proofCaptureRepository = proofCaptureRepository,
        scanSource = scanSource,
        proofCaptureSource = proofCaptureSource,
        bootstrapRepository = bootstrapRepository,
        savedStateHandle = SavedStateHandle(if (taskId != null) mapOf("taskId" to taskId) else emptyMap()),
    )

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
        advanceUntilIdle()

        val request = sync.lastRequest
        assertEquals("real captured answers must travel in the submit payload", JsonPrimitive(true), request?.answers?.get("cold_chain_verified"))
        assertEquals(JsonPrimitive("2"), request?.answers?.get("dose_ml_given"))
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
        advanceUntilIdle()

        val answer = sync.lastRequest?.answers?.get("goat_scan") as? JsonArray
        assertEquals(listOf(JsonPrimitive("TAG-100"), JsonPrimitive("TAG-101")), answer)
        // Submitting stops the active scan — hardware capture is never left running behind
        // a screen that just enqueued its write.
        assertEquals(1, scanSource.stopCount)
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
        advanceUntilIdle()
        assertNull("submit must not enqueue a payload with a local Room proof id", sync.lastRequest)

        val localProofId = viewModel.state.value.formRunner?.fields?.single()?.proofItems?.single()?.id
        proofCaptureRepository.markSynced(localProofId!!, "server-proof-administration")
        advanceUntilIdle()

        assertTrue("submit must unblock once the captured proof has a completed backend proof id", viewModel.state.value.canSubmit)
        assertNull(viewModel.state.value.formRunner?.blockedReason)

        viewModel.onEvent(SubmitEvent.Submit)
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
        advanceUntilIdle()
        assertNull(sync.lastRequest)

        val proofId = viewModel.state.value.formRunner?.fields?.single { it.key == "extra_video" }?.proofItems?.single()?.id
        proofCaptureRepository.markSynced(proofId!!, "server-proof-extra")
        advanceUntilIdle()

        assertTrue(viewModel.state.value.canSubmit)
        viewModel.onEvent(SubmitEvent.Submit)
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
    fun `cold cache, no-task-assigned, and task-load-failed states never render fixture farm identity`() = runTest(dispatcher) {
        // Cold cache: task id present, Room + network both never answer (never call refreshTaskDetail
        // successfully) — exercised via a repository whose Flow never emits real data.
        val stuckRepository = object : TasksRepository {
            override suspend fun tasks(state: String?, limit: Int?): TaskListResponseDto = error("unused")
            override suspend fun taskDetail(taskId: String): TaskDetail = error("unused")
            override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> =
                MutableStateFlow(Resource(data = null))
            override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = Result.failure(IllegalStateException("offline"))
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
            bootstrapRepository = FakeCaptureBootstrapRepository(profile = null),
        )
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()
        assertTrue(viewModel.state.value.isCaptureRoleBlocked)
        assertFalse(viewModel.state.value.canSubmit)

        viewModel.onEvent(SubmitEvent.FormToggle("cold_chain_verified", true))
        viewModel.onEvent(SubmitEvent.Submit)
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
) : TasksRepository {
    private val flow = MutableStateFlow(Resource<TaskDetail>(data = null))

    override suspend fun tasks(state: String?, limit: Int?): TaskListResponseDto = error("unused")

    override suspend fun taskDetail(taskId: String): TaskDetail = TaskDetail(task = task, form = form)

    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> = flow

    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = runCatching {
        flow.value = Resource(data = TaskDetail(task = task, form = form), lastSyncedAt = 1L)
    }
}

private class CapturingSyncRepository : SyncRepository {
    var lastRequest: SubmitTaskRequestDto? = null
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status

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

    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")

    override suspend fun triggerDrain() = Unit
}
