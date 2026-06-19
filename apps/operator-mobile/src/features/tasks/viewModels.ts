import type { TaskListResponse, TaskResponse } from "../../shared/api/client.js";

export type TaskListItemModel = {
  taskId: string;
  title: string;
  subtitle: string;
  state: string;
  priority: string;
  blocked: boolean;
};

export function buildTaskListModel(response: TaskListResponse): TaskListItemModel[] {
  return response.items.map((task) => ({
    taskId: task.task_id,
    title: task.title,
    subtitle: `${task.sop_code} · ${task.scope_type}`,
    state: task.state,
    priority: task.priority,
    blocked: task.state === "accepted" || task.state === "rejected" || task.state === "canceled",
  }));
}

export function buildTaskDetailModel(response: TaskResponse) {
  return {
    taskId: response.task.task_id,
    title: response.task.title,
    state: response.task.state,
    sopVersionId: response.task.sop_version_id,
    sopTitle: response.sop_version?.form_dsl?.title ? String(response.sop_version.form_dsl.title) : response.task.sop_code,
    canExecute: ["assigned", "in_progress", "rework_requested"].includes(response.task.state),
    proofReviewRequired: Boolean(response.sop_version?.proof_policy?.verify_before_apply),
    submissionCount: response.submissions?.length ?? 0,
  };
}
