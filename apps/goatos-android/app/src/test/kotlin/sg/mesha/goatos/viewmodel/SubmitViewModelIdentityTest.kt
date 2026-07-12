package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
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
            repository,
            NoopSyncRepository(),
            SavedStateHandle(mapOf("taskId" to "task-selected")),
        )

        advanceUntilIdle()

        assertEquals("task-selected", repository.detailTaskId)
        assertEquals(0, repository.listCalls)
        assertFalse(viewModel.state.value.isTaskLoadFailed)
    }
}

private class CapturingTasksRepository : TasksRepository {
    var listCalls = 0
    var detailTaskId: String? = null

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
}

private class NoopSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun triggerDrain() = Unit
}
