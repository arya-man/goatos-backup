"use server";

import { revalidatePath } from "next/cache";
import { actionErrorMessage, actionRedirect } from "@/lib/action-helpers";
import { acceptVaccinationCompletion, rejectVaccinationCompletion } from "@/lib/api/server";

// A verify/reject ripples across every screen that reads the process-integrity model: the Action Center
// board + verify queue, PHC Vaccination ops, Protocol Adherence, and the Control Tower summary.
function revalidateVaccinationViews(): void {
  for (const p of ["/action-center", "/vaccination", "/protocol-adherence", "/workflows", "/"]) {
    revalidatePath(p);
  }
}

// verifyCompletionAction accepts a recorded completion (SM-5 verify): completes the obligation +
// consumes the reserved dose. Bound to the Verify button in the verification queue.
export async function verifyCompletionAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.verify_accepted";
  try {
    const completionId = String(formData.get("completion_id") ?? "");
    if (!completionId) throw new Error("completion_id is required");
    const result = await acceptVaccinationCompletion(completionId);
    if (!result.ok) {
      status = "error";
      actionKey = actionErrorMessage(result.error);
    } else {
      actionKey = result.data.applied ? "action.verify_accepted" : "action.verify_replay";
      revalidateVaccinationViews();
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}

// rejectCompletionAction rejects (reason="rejected") or requests rework (reason="rework_requested").
export async function rejectCompletionAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.completion_rejected";
  try {
    const completionId = String(formData.get("completion_id") ?? "");
    const reason = String(formData.get("reason") ?? "rejected");
    if (!completionId) throw new Error("completion_id is required");
    const result = await rejectVaccinationCompletion(completionId, reason);
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
