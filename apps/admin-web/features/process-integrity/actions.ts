"use server";

import { revalidatePath } from "next/cache";
import { actionErrorMessage, actionRedirect } from "@/lib/action-helpers";
import { requestSopTaskRework, resolveVaccinationStageReviewItem, verifySopTask } from "@/lib/api/server";

// A verify/reject ripples across every screen that reads the process-integrity model: the Action Center
// board + verify queue, Preventive Care (PC) Vaccination ops, Protocol Adherence, and the Control Tower summary.
function revalidateVaccinationViews(): void {
  for (const p of ["/action-center", "/vaccination", "/protocol-adherence", "/workflows", "/"]) {
    revalidatePath(p);
  }
}

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
      revalidateVaccinationViews();
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
      revalidateVaccinationViews();
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}

export async function resolveStageReviewAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.stage_review_resolved";
  try {
    const reviewItemId = String(formData.get("review_item_id") ?? "");
    const resolution = String(formData.get("resolution") ?? "");
    const note = String(formData.get("note") ?? "").trim();
    if (!reviewItemId || (resolution !== "corrected" && resolution !== "exception") || !note) {
      throw new Error("stage review resolution, note, and item id are required");
    }
    const result = await resolveVaccinationStageReviewItem(reviewItemId, { resolution, note });
    if (!result.ok) {
      status = "error";
      actionKey = actionErrorMessage(result.error);
    } else {
      revalidateVaccinationViews();
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}
