package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto

/**
 * Operator Tasks screen area: the assigned-task queue and idempotent SOP task
 * submission. Thin pass-through over [AppApi]; DTO -> UiState mapping in :app.
 */
interface TasksRepository {
    suspend fun tasks(
        state: String? = null,
        limit: Int? = null,
    ): TaskListResponseDto

    suspend fun submit(
        taskId: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto
}

class DefaultTasksRepository(
    private val api: AppApi,
) : TasksRepository {
    override suspend fun tasks(
        state: String?,
        limit: Int?,
    ): TaskListResponseDto = api.listAppTasks(state, limit)

    override suspend fun submit(
        taskId: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto = api.submitAppTask(taskId, request.idempotencyKey, request)
}
