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
  let message = "";
  try {
    const completionId = String(formData.get("completion_id") ?? "");
    if (!completionId) throw new Error("completion_id is required");
    const result = await acceptVaccinationCompletion(completionId);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = result.data.applied ? "Verification accepted." : "Verification was already applied.";
      revalidateVaccinationViews();
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to verify completion.";
  }
  actionRedirect(formData, status, message);
}

// rejectCompletionAction rejects (reason="rejected") or requests rework (reason="rework_requested").
export async function rejectCompletionAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const completionId = String(formData.get("completion_id") ?? "");
    const reason = String(formData.get("reason") ?? "rejected");
    if (!completionId) throw new Error("completion_id is required");
    const result = await rejectVaccinationCompletion(completionId, reason);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = reason === "rework_requested" ? "Rework requested." : "Completion rejected.";
      revalidateVaccinationViews();
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to reject completion.";
  }
  actionRedirect(formData, status, message);
}
