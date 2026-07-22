"use server";

import { actionErrorMessage, actionRedirect } from "@/lib/action-helpers";
import { requestSopTaskRework, verifySopTask } from "@/lib/api/server";
import { revalidateVaccinationCommandLenses } from "@/lib/vaccination-command-lenses";

// verifyCompletionAction reviews the SOP task, then the backend SOP review fanout applies SM-5:
// accept completion, complete the obligation, consume reserved stock, and emit durable completion.
export async function verifyCompletionAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.verify_accepted";
  try {
    const taskId = String(formData.get("task_id") ?? "");
    const rowVersion = Number(formData.get("row_version") ?? 0);
    if (!taskId || rowVersion <= 0) throw new Error("task review handle is required");
    const result = await verifySopTask(taskId, { reason: "accepted", row_version: rowVersion });
    if (!result.ok) {
      status = "error";
      actionKey = actionErrorMessage(result.error);
    } else {
      actionKey = "action.verify_accepted";
      revalidateVaccinationCommandLenses();
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}

// rejectCompletionAction requests SOP rework. The reason preserves whether the operator clicked the
// reject or rework control while keeping SOP task state and review fanout canonical.
export async function rejectCompletionAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.completion_rejected";
  try {
    const taskId = String(formData.get("task_id") ?? "");
    const rowVersion = Number(formData.get("row_version") ?? 0);
    const reason = String(formData.get("reason") ?? "rejected");
    if (!taskId || rowVersion <= 0) throw new Error("task review handle is required");
    const result = await requestSopTaskRework(taskId, { reason, row_version: rowVersion });
    if (!result.ok) {
      status = "error";
      actionKey = actionErrorMessage(result.error);
    } else {
      actionKey = reason === "rework_requested" ? "action.rework_requested" : "action.completion_rejected";
      revalidateVaccinationCommandLenses();
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}
