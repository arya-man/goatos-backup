package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.forms.toFormSpec
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.SubmissionSummaryDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto

/** A task opened for recording: the summary, its parsed [form] to render, and prior submissions. */
data class TaskDetail(
    val task: TaskSummaryDto,
    val form: FormSpec,
    val submissions: List<SubmissionSummaryDto> = emptyList(),
)

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

    /** Opens one task WITH its SOP form (parsed from `form_dsl`) so the runner can render it. */
    suspend fun taskDetail(taskId: String): TaskDetail
}

class DefaultTasksRepository(
    private val api: AppApi,
) : TasksRepository {
    override suspend fun tasks(
        state: String?,
        limit: Int?,
    ): TaskListResponseDto = api.listAppTasks(state, limit)

    override suspend fun taskDetail(taskId: String): TaskDetail {
        val detail = api.getAppTask(taskId)
        return TaskDetail(
            task = detail.task,
            form = detail.sopVersion?.toFormSpec() ?: FormSpec.Empty,
            submissions = detail.submissions,
        )
    }
}
