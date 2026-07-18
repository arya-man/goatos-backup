package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import android.view.KeyEvent
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
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
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.capture.ROSTER_SCAN_FIELD_KEY
import sg.mesha.goatos.core.data.capture.RfidScanAttemptOutcome
import sg.mesha.goatos.core.data.capture.RfidScanTagRole
import sg.mesha.goatos.core.data.forms.FormField
import sg.mesha.goatos.core.data.forms.FormFieldType
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterRowDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
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
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("TAG-100")
        advanceUntilIdle()

        assertEquals(listOf("TAG-100"), scanCaptures.tagsForTask("task-1"))
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED), scanAttempts.calls.map { it.outcome })
        assertEquals(ScanStatus.DONE, scanVm.state.value.roster.single().status)

        val submitSync = CapturingSubmitSyncRepository()
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
            proofCaptureRepository = FakeProofCaptureRepository(),
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
        assertEquals(listOf(JsonPrimitive("TAG-100")), answer)
    }

    @Test
    fun `repeated RFID scan is not recorded as another capture`() = runTest(dispatcher) {
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
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("TAG-100")
        reader.emit("TAG-100")
        advanceUntilIdle()

        assertEquals(listOf("TAG-100"), scanCaptures.tagsForTask("task-1"))
        assertEquals("duplicate hardware reads should not re-record the same roster tag", 1, scanCaptures.recordScanCalls)
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED, RfidScanAttemptOutcome.DUPLICATE), scanAttempts.calls.map { it.outcome })
        assertEquals(2, scanVm.state.value.feed.size)
        assertEquals(ScanStatus.DONE, scanVm.state.value.roster.single().status)
    }

    @Test
    fun `secondary tag for same goat records duplicate attempt but does not count twice`() = runTest(dispatcher) {
        val scanCaptures = FakeScanCaptureRepository()
        val scanAttempts = FakeScanAttemptRepository()
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
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        reader.emit("901007000504418")
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
        assertTrue(scanVm.state.value.feed.first().vaccineLabel.contains("already scanned"))
    }

    @Test
    fun `manual ring tap does not persist a scan capture but later reader tag does`() = runTest(dispatcher) {
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
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { scanVm.state.collect {} }
        advanceUntilIdle()

        scanVm.onEvent(ScanEvent.Tap)
        advanceUntilIdle()

        assertEquals(ScanStatus.DONE, scanVm.state.value.roster.single().status)
        assertEquals(emptyList<String>(), scanCaptures.tagsForTask("task-1"))

        reader.emit("TAG-100")
        advanceUntilIdle()

        assertEquals(listOf("TAG-100"), scanCaptures.tagsForTask("task-1"))
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED), scanAttempts.calls.map { it.outcome })
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
    fun `paginated scan roster exposes load more and scans a loaded page two tag`() = runTest(dispatcher) {
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
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("shedId" to "shed-1", "taskId" to "task-1")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue("page-one roster must expose the continuation affordance", vm.state.value.hasMore)
        assertFalse("cannot submit while the server says another roster page exists", vm.state.value.canSubmit)

        vm.onEvent(ScanEvent.LoadMore)
        advanceUntilIdle()

        assertEquals(listOf("TAG-001", "TAG-200"), vm.state.value.roster.map { it.primaryTag })
        assertFalse(vm.state.value.hasMore)

        reader.emit("TAG-200")
        advanceUntilIdle()

        val loadedPageTwoRow = vm.state.value.roster.single { it.primaryTag == "TAG-200" }
        assertEquals(ScanStatus.DONE, loadedPageTwoRow.status)
        assertEquals(listOf("TAG-200"), scanCaptures.tagsForTask("task-1"))
        assertEquals(listOf(RfidScanAttemptOutcome.ACCEPTED), scanAttempts.calls.map { it.outcome })
        assertFalse(vm.state.value.feed.any { it.status == ScanStatus.SKIPPED && it.primaryTag == "TAG-200" })
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
    override fun onKeyEvent(event: KeyEvent): Boolean = false
}

private class FakeScanExecutionRepository(
    private val firstPage: ScanRosterResponseDto,
    private val continuationPages: Map<String, ScanRosterResponseDto> = emptyMap(),
) : ExecutionRepository {
    private val scanRoster = MutableStateFlow(Resource<ScanRosterResponseDto>(data = null))

    override fun observeScanRoster(shedId: String, taskId: String?, limit: Int?): Flow<Resource<ScanRosterResponseDto>> = scanRoster

    private fun norm(tag: String): String = tag.filter { it.isLetterOrDigit() }.lowercase()

    private fun allRows() = scanRoster.value.data?.rows.orEmpty()

    override suspend fun findScanRosterByTag(
        shedId: String,
        normalizedTag: String,
    ): sg.mesha.goatos.core.data.cache.ScanRosterRowEntity? =
        allRows().firstOrNull {
            norm(it.primaryTag) == normalizedTag || it.secondaryTag?.let { s -> norm(s) == normalizedTag } == true
        }?.let {
            sg.mesha.goatos.core.data.cache.ScanRosterRowEntity(
                id = "$shedId#${it.obligationId}",
                shedId = shedId,
                goatId = it.goatId,
                primaryTag = it.primaryTag,
                secondaryTag = it.secondaryTag,
                vaccineLabel = it.vaccineLabel,
                status = it.status,
                obligationId = it.obligationId,
                updatedAt = 0L,
            )
        }

    override fun observeScanRosterStatusCounts(
        shedId: String,
    ): Flow<List<sg.mesha.goatos.core.data.cache.StatusCount>> =
        scanRoster.map { resource ->
            resource.data?.rows.orEmpty()
                .groupingBy { it.status }
                .eachCount()
                .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }
        }

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        obligationIds: List<String>,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> =
        allRows().filter { it.obligationId in obligationIds }
            .groupingBy { it.status }
            .eachCount()
            .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }

    override suspend fun getScanRosterStatusCounts(
        shedId: String,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> =
        allRows().groupingBy { it.status }
            .eachCount()
            .map { (status, count) -> sg.mesha.goatos.core.data.cache.StatusCount(status, count) }

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?): Result<Unit> = runCatching {
        scanRoster.value = Resource(data = firstPage, lastSyncedAt = 1L)
    }

    override suspend fun appendScanRoster(shedId: String, taskId: String?, cursor: String, limit: Int?): Result<Unit> = runCatching {
        val current = scanRoster.value.data ?: error("no first page")
        val page = continuationPages[cursor] ?: error("missing page")
        scanRoster.value = Resource(
            data = page.copy(rows = current.rows + page.rows),
            lastSyncedAt = 2L,
        )
    }

    override suspend fun rows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, limit: Int?, cursor: String?,
    ): VaccinationExecutionResponseDto = error("unused")

    override fun observeRows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, limit: Int?,
    ): Flow<Resource<VaccinationExecutionResponseDto>> = error("unused")

    override suspend fun refreshRows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, limit: Int?,
    ): Result<Unit> = error("unused")

    override suspend fun appendRows(
        cursor: String, parkId: String?, workState: String?, asOf: String?, dueBefore: String?, limit: Int?,
    ): Result<Unit> = error("unused")

    override suspend fun shed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?): VaccinationExecutionShedDrilldownDto =
        error("unused")

    override fun observeShed(
        shedId: String, asOf: String?, dueBefore: String?, limit: Int?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> = error("unused")

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?): Result<Unit> =
        error("unused")

    override suspend fun scanRoster(shedId: String, taskId: String?, cursor: String?, limit: Int?): ScanRosterResponseDto =
        error("unused")
}

private class FakeTaskRepository(
    private val task: TaskSummaryDto,
    private val form: FormSpec,
) : TasksRepository {
    private val taskDetail = MutableStateFlow(Resource<TaskDetail>(data = null))

    override suspend fun tasks(state: String?, limit: Int?): TaskListResponseDto = error("unused")
    override suspend fun taskDetail(taskId: String): TaskDetail = TaskDetail(task = task, form = form)
    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> = taskDetail
    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = runCatching {
        taskDetail.value = Resource(data = TaskDetail(task = task, form = form), lastSyncedAt = 1L)
    }
}

private class CapturingSubmitSyncRepository : SyncRepository {
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
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> =
        error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun triggerDrain() = Unit
}
