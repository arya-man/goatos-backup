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
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
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
            savedStateHandle = SavedStateHandle(mapOf("taskId" to "task-selected")),
        )
        // MOB-010: the Room task-detail observer is now gated on `state` having a subscriber —
        // keep it hot for the duration of the test the same way a real screen would.
        backgroundScope.launch { viewModel.state.collect {} }

        advanceUntilIdle()

        assertEquals("task-selected", repository.detailTaskId)
        assertEquals(0, repository.listCalls)
        assertFalse(viewModel.state.value.isTaskLoadFailed)
    }
}

private class CapturingTasksRepository : TasksRepository {
    var listCalls = 0
    var detailTaskId: String? = null
    private val detailFlow = MutableStateFlow(Resource<TaskDetail>(data = null))

    override suspend fun tasks(state: String?, limit: Int?): TaskListResponseDto {
        listCalls++
        error("submit must not select the first task")
    }

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
}

private class NoopSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun triggerDrain() = Unit
}
