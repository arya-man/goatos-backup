"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import {
  getProofDownloadUrl,
  getToxinTask,
  recordToxinVerdict,
  type ToxinTaskDetail,
} from "@/lib/api/server";

const PATHNAME = "/verify";

function redirectTarget(formData: FormData): URL {
  const returnTo = String(formData.get("return_to") ?? PATHNAME);
  const safe = returnTo.startsWith(PATHNAME) ? returnTo : PATHNAME;
  return new URL(safe, "https://admin.mesha.local");
}

// Same redirect-feedback shape as ./actions.ts (va_status/va_code), under toxin-scoped params so a
// toxin verdict's banner can never be mistaken for a verification-queue verdict's.
function withFeedback(url: URL, status: "success" | "error", code: string): string {
  url.searchParams.set("tx_status", status);
  url.searchParams.set("tx_code", code);
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

export type ToxinDetailLoad =
  | {
      ok: true;
      detail: ToxinTaskDetail;
      /** Signed browser-usable URL per proof_ref (done steps + strip photo); null when unresolved. */
      proofUrls: Record<string, string | null>;
    }
  | { ok: false; code: string };

// loadToxinTaskDetailAction feeds the toxin drawer: the 7-step detail plus a signed download URL
// per proof. Bounded fan-out — a task has at most 6 step proofs and 1 strip photo — resolved in
// parallel. An unresolved proof degrades to a step row with no media link, never an error page.
export async function loadToxinTaskDetailAction(taskId: string): Promise<ToxinDetailLoad> {
  const detail = await getToxinTask(taskId);
  if (!detail.ok) return { ok: false, code: detail.error.code ?? detail.error.kind };
  const refs = new Set<string>();
  for (const step of detail.data.steps) {
    if (step.proof_ref) refs.add(step.proof_ref);
  }
  if (detail.data.strip_photo_ref) refs.add(detail.data.strip_photo_ref);
  const entries = await Promise.all(
    [...refs].map(async (ref) => [ref, await getProofDownloadUrl(ref)] as const),
  );
  return { ok: true, detail: detail.data, proofUrls: Object.fromEntries(entries) };
}

// recordToxinVerdictAction is the CEO/CXO act on a submitted toxin test: Accept closes the round;
// Reject (reason REQUIRED) cancels it and the backend mints a retest. The idempotency key is
// derived, not random — the same (task, row_version, decision) is one logical act, and row_version
// makes the key self-expiring once the verdict lands.
export async function recordToxinVerdictAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const taskId = String(formData.get("task_id") ?? "").trim();
  const decision = String(formData.get("decision") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  const rowVersion = Number(formData.get("row_version") ?? "");

  if (!taskId || (decision !== "accept" && decision !== "reject")) {
    redirect(withFeedback(url, "error", !taskId ? "missing_task_handle" : "invalid_decision"));
  }
  if (!Number.isInteger(rowVersion) || rowVersion < 1) {
    redirect(withFeedback(url, "error", "missing_row_version"));
  }
  // The backend owns this rule (400 reject_reason_required); checking here too saves a round trip.
  if (decision === "reject" && !reason) {
    redirect(withFeedback(url, "error", "reject_reason_required"));
  }

  const idempotencyKey = `toxin-verdict-${taskId}-${rowVersion}-${decision}`;
  const result = await recordToxinVerdict(
    taskId,
    {
      decision: decision as "accept" | "reject",
      // reason travels only on a reject: on an accept it is a value the contract does not ask for.
      ...(decision === "reject" ? { reason } : {}),
      row_version: rowVersion,
    },
    idempotencyKey,
  );
  revalidatePath(PATHNAME);
  if (!result.ok) {
    // version_conflict surfaces as "reload and review again" copy (toxin.feedback.version_conflict).
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", decision === "accept" ? "verdict_accepted" : "verdict_rejected"));
}
