"use server";

import { actionErrorMessage, actionRedirect, requiredNumber, requiredString } from "@/lib/action-helpers";
import {
  assignTask,
  createTask,
  reworkTask,
  verifyTask,
  type AssignTaskRequest,
  type CreateTaskRequest,
  type ReviewTaskRequest,
} from "@/lib/api/server";

export async function createTaskAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const assignedTo = stringField(formData, "assigned_to");
    const body: CreateTaskRequest = {
      sop_code: stringField(formData, "sop_code") ?? "shifting",
      task_type: stringField(formData, "task_type") ?? "shifting_direction",
      title: requiredString(formData, "title"),
      description: stringField(formData, "description") ?? "",
      assigned_to: assignedTo ?? null,
      scope_type: stringField(formData, "scope_type") ?? "park",
      scope_id: requiredString(formData, "scope_id"),
      priority: stringField(formData, "priority") ?? "normal",
      context: {
        source: "admin-web",
        category: stringField(formData, "category") ?? "routine",
      },
    };
    const result = await createTask(body);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.task.title} created.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to create task.";
  }
  actionRedirect(formData, status, message);
}

export async function assignTaskAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: AssignTaskRequest = {
      assigned_to: requiredString(formData, "assigned_to"),
      reason: stringField(formData, "reason") ?? "Assigned from task queue.",
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await assignTask(requiredString(formData, "task_id"), body);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.task.title} assigned.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to assign task.";
  }
  actionRedirect(formData, status, message);
}

export async function verifyTaskAction(formData: FormData) {
  await reviewAction(formData, "verify");
}

export async function reworkTaskAction(formData: FormData) {
  await reviewAction(formData, "rework");
}

async function reviewAction(formData: FormData, action: "verify" | "rework") {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: ReviewTaskRequest = {
      reason: stringField(formData, "reason") ?? (action === "verify" ? "Proof accepted." : "Proof requires rework."),
      row_version: requiredNumber(formData, "row_version"),
    };
    const taskId = requiredString(formData, "task_id");
    const result = action === "verify" ? await verifyTask(taskId, body) : await reworkTask(taskId, body);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.task.title} moved to ${result.data.task.state}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : `Unable to ${action} task.`;
  }
  actionRedirect(formData, status, message);
}

function stringField(formData: FormData, key: string): string | undefined {
  const value = formData.get(key);
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
}
