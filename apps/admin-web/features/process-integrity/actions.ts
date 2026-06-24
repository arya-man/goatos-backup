"use server";

import { revalidatePath } from "next/cache";
import { acceptVaccinationCompletion, rejectVaccinationCompletion } from "@/lib/api/server";

function redirectParams(result: { applied?: boolean }, ok: boolean, action: string): string {
  const status = ok ? "success" : "error";
  const message = ok ? `${action} applied` : `${action} failed`;
  return `?action_status=${status}&action_message=${encodeURIComponent(message)}`;
}

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
  const completionId = String(formData.get("completion_id") ?? "");
  if (!completionId) return;
  const result = await acceptVaccinationCompletion(completionId);
  revalidateVaccinationViews();
  void redirectParams(result.ok ? result.data : {}, result.ok, "verify");
}

// rejectCompletionAction rejects (reason="rejected") or requests rework (reason="rework_requested").
export async function rejectCompletionAction(formData: FormData): Promise<void> {
  const completionId = String(formData.get("completion_id") ?? "");
  const reason = String(formData.get("reason") ?? "rejected");
  if (!completionId) return;
  await rejectVaccinationCompletion(completionId, reason);
  revalidateVaccinationViews();
}
