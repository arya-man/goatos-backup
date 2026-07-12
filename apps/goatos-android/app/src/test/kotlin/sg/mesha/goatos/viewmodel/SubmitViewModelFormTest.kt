package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
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

/**
 * MOB-002 + MOB-005 guardrails for the Submit shed-record screen.
 *
 * MOB-002 (`SubmitTaskRequestDto` sent with only `sopVersionId`/`idempotencyKey`, real answers
 * never captured): proves a required text/boolean recording-form field blocks submit until
 * answered, and that the ANSWERED value actually travels inside the enqueued
 * [SubmitTaskRequestDto.answers] — not silently dropped.
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
        val viewModel = SubmitViewModel(repository, sync, SavedStateHandle(mapOf("taskId" to "task-1")))

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
    fun `a required proof field with no capture pipeline stays honestly blocked and never enqueues`() = runTest(dispatcher) {
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
        val viewModel = SubmitViewModel(repository, sync, SavedStateHandle(mapOf("taskId" to "task-2")))

        advanceUntilIdle()
        assertFalse(viewModel.state.value.canSubmit)
        assertEquals(
            "Record the required proof before submitting — not available in this build.",
            viewModel.state.value.formRunner?.blockedReason,
        )

        viewModel.onEvent(SubmitEvent.Submit)
        advanceUntilIdle()

        assertNull("a client-side-blocked submission must never reach the outbox", sync.lastRequest)
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
        val coldCacheVm = SubmitViewModel(stuckRepository, CapturingSyncRepository(), SavedStateHandle(mapOf("taskId" to "task-x")))
        advanceUntilIdle()
        assertFixtureFree(coldCacheVm.state.value)
        assertTrue(coldCacheVm.state.value.isTaskLoadFailed)

        // No task assigned: taskId is blank.
        val noTaskVm = SubmitViewModel(stuckRepository, CapturingSyncRepository(), SavedStateHandle(emptyMap()))
        advanceUntilIdle()
        assertFixtureFree(noTaskVm.state.value)
        assertTrue(noTaskVm.state.value.isNoTaskAssigned)
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
    ): AppResult<String> = error("unused")

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")

    override suspend fun triggerDrain() = Unit
}
