"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { assignSopTask, getSopTask, recordVerificationVerdict, requestSopTaskRework, type VerificationDecision } from "@/lib/api/server";

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

// recordVerificationVerdictAction is the VERIFIER's act: approve or reject the proof video itself.
// It is the counterpart to the two authority actions below, which touch the source SOP task instead.
//
// row_version comes from the rendered item because it guards THAT row (unlike the rework/reassign
// actions below, whose row_version belongs to a different row and so must be re-fetched). A stale
// value is the correct failure here: it means someone recorded a verdict since this page rendered,
// and the backend's 409 is what stops this submit from silently overwriting it.
export async function recordVerificationVerdictAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const itemId = String(formData.get("item_id") ?? "").trim();
  const decision = String(formData.get("decision") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  const rowVersion = Number(formData.get("row_version") ?? "");

  if (!itemId || (decision !== "approved" && decision !== "rejected")) {
    redirect(withFeedback(url, "error", !itemId ? "missing_item_handle" : "invalid_decision"));
  }
  if (!Number.isInteger(rowVersion) || rowVersion < 1) {
    redirect(withFeedback(url, "error", "missing_row_version"));
  }
  // The backend owns this rule (422 on a reasonless rejection); checking here too keeps the
  // operator from losing a page round-trip to learn it.
  if (decision === "rejected" && !reason) {
    redirect(withFeedback(url, "error", "missing_reason"));
  }

  // Idempotency identity for this verdict. Derived, not random, so a double-click or a retried
  // Server Action is ONE write: the same (item, row_version, decision) is the same logical act.
  // row_version makes it self-expiring — once the verdict lands the row moves on, so a later,
  // legitimately different verdict can never collide with this key.
  const idempotencyKey = `verification-verdict-${itemId}-${rowVersion}-${decision}`;
  const result = await recordVerificationVerdict(
    itemId,
    {
      decision: decision as VerificationDecision,
      // Send reason only when rejecting: the schema marks it required-when-rejected, and an empty
      // string on an approval is a value the contract does not ask for.
      ...(decision === "rejected" ? { reason } : {}),
      row_version: rowVersion,
    },
    idempotencyKey,
  );
  revalidateVaccinationViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", decision === "approved" ? "verdict_approved" : "verdict_rejected"));
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
