package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.TaskListResponseDto

/**
 * Operator Tasks screen area: assigned-task reads only. Mutating task submissions must go
 * through SyncRepository's durable outbox; keep this repository free of a direct submit escape
 * hatch so callers cannot bypass offline replay, idempotency checks, or sync-status reporting.
 */
interface TasksRepository {
    suspend fun tasks(
        state: String? = null,
        limit: Int? = null,
    ): TaskListResponseDto
}

class DefaultTasksRepository(
    private val api: AppApi,
) : TasksRepository {
    override suspend fun tasks(
        state: String?,
        limit: Int?,
    ): TaskListResponseDto = api.listAppTasks(state, limit)
}
