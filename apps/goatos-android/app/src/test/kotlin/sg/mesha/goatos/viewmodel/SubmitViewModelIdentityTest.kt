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
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto

@OptIn(ExperimentalCoroutinesApi::class)
class SubmitViewModelIdentityTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `submit loads exact route task and never selects first assigned task`() = runTest(dispatcher) {
        val repository = CapturingTasksRepository()
        val viewModel = SubmitViewModel(
            repo = repository,
            syncRepository = NoopSyncRepository(),
            scanCaptureRepository = FakeScanCaptureRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            scanSource = sg.mesha.goatos.rfid.FakeScanSource(),
            proofCaptureSource = sg.mesha.goatos.capture.FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
        analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
        crashReporter = sg.mesha.goatos.core.analytics.NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("taskId" to "task-selected")),
        )
        // MOB-010: the Room task-detail observer is now gated on `state` having a subscriber —
        // keep it hot for the duration of the test the same way a real screen would.
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()

        assertEquals("task-selected", repository.detailTaskId)
        assertFalse(viewModel.state.value.isTaskLoadFailed)
    }

    @Test
    fun `shed submit idempotency key is scoped by shed`() {
        val oldYashoda = TaskSummaryDto(taskId = "task-1", sopVersionId = "sop-1", scopeId = "old-yashoda", rowVersion = 1)
        val godelOne = TaskSummaryDto(taskId = "task-1", sopVersionId = "sop-1", scopeId = "godel-1", rowVersion = 1)

        assertEquals("shed-submit:task-1:scope:old-yashoda:rv:1", SubmitViewModel.stableSubmissionKey(oldYashoda))
        assertEquals("shed-submit:task-1:scope:godel-1:rv:1", SubmitViewModel.stableSubmissionKey(godelOne))
    }
}

private class CapturingTasksRepository : TasksRepository {
    var detailTaskId: String? = null
    private val detailFlow = MutableStateFlow(Resource<TaskDetail>(data = null))

    override suspend fun taskDetail(taskId: String): TaskDetail {
        detailTaskId = taskId
        return TaskDetail(
            task = TaskSummaryDto(taskId = taskId, sopVersionId = "sop-1", scopeId = "shed-1", rowVersion = 7),
            form = FormSpec.Empty,
        )
    }

    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> = detailFlow

    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = runCatching {
        val detail = taskDetail(taskId)
        detailFlow.value = Resource(data = detail, lastSyncedAt = 1L)
    }

    override fun observeShedCompletionSummary(taskId: String, shedId: String?): Flow<ShedCompletionSummaryDto?> =
        MutableStateFlow(null)

    override suspend fun refreshShedCompletionSummary(taskId: String, shedId: String?): Result<Unit> = Result.success(Unit)
}

private class NoopSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): kotlinx.coroutines.flow.Flow<SyncQueueItem?> = kotlinx.coroutines.flow.flowOf(null)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun triggerDrain() = Unit
}
