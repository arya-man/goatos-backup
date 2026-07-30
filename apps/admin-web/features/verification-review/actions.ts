"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { assignSopTask, getSopTask, requestSopTaskRework } from "@/lib/api/server";

const PATHNAME = "/actions";

// A rework/re-assign on the source SOP task ripples across every screen that reads the
// process-integrity model (same fan-out as features/process-integrity/actions.ts).
function revalidateVaccinationViews(): void {
  for (const p of [PATHNAME, "/action-center", "/vaccination", "/protocol-adherence", "/workflows", "/"]) {
    revalidatePath(p);
  }
}

function redirectTarget(formData: FormData): URL {
  const returnTo = String(formData.get("return_to") ?? PATHNAME);
  const safe = returnTo.startsWith(PATHNAME) ? returnTo : PATHNAME;
  return new URL(safe, "https://admin.mesha.local");
}

function withFeedback(url: URL, status: "success" | "error", code: string): string {
  url.searchParams.set("va_status", status);
  url.searchParams.set("va_code", code);
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

// reworkVerificationItemAction requests SOP rework on the verification item's SOURCE task. The
// verifier's rejected verdict is advisory; this is the authority's real act. Fetches the task's
// CURRENT row_version first (the verification_item's row_version guards a different row) so the
// optimistic-concurrency write does not race a stale value carried in the form.
export async function reworkVerificationItemAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const taskId = String(formData.get("task_id") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  if (!taskId || !reason) {
    redirect(withFeedback(url, "error", !taskId ? "missing_task_handle" : "missing_reason"));
  }

  const task = await getSopTask(taskId);
  if (!task.ok) {
    redirect(withFeedback(url, "error", task.error.code ?? task.error.kind));
  }

  const result = await requestSopTaskRework(taskId, { reason, row_version: task.data.task.row_version });
  revalidateVaccinationViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", "rework_requested"));
}

// reassignVerificationItemAction assigns the verification item's SOURCE task to a different staff
// position seat, the authority's "re-assign owner" act.
export async function reassignVerificationItemAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const taskId = String(formData.get("task_id") ?? "").trim();
  const assignedTo = String(formData.get("assigned_to") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  if (!taskId || !assignedTo || !reason) {
    redirect(
      withFeedback(url, "error", !taskId ? "missing_task_handle" : !assignedTo ? "missing_assignee" : "missing_reason"),
    );
  }

  const task = await getSopTask(taskId);
  if (!task.ok) {
    redirect(withFeedback(url, "error", task.error.code ?? task.error.kind));
  }

  const result = await assignSopTask(taskId, { assigned_to: assignedTo, reason, row_version: task.data.task.row_version });
  revalidateVaccinationViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", "reassigned"));
}
