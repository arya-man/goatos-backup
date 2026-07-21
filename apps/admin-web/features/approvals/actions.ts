"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { decideAdminWebApproval } from "@/lib/api/server";

const PATHNAME = "/approvals";

// Deciding a request applies (approve) or refuses (reject) a birth/death/shifting change, which
// ripples across every screen that reads the counts / herd-register / process model.
function revalidateApprovalViews(): void {
  for (const p of [PATHNAME, "/counts/herd", "/counts/breakdown", "/action-center", "/"]) {
    revalidatePath(p);
  }
}

function redirectTarget(formData: FormData): URL {
  const returnTo = String(formData.get("return_to") ?? PATHNAME);
  const safe = returnTo.startsWith(PATHNAME) ? returnTo : PATHNAME;
  return new URL(safe, "https://admin.mesha.local");
}

function withFeedback(url: URL, status: "success" | "error", code: string): string {
  url.searchParams.set("ap_status", status);
  url.searchParams.set("ap_code", code);
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

// A stable idempotency key per (request, verb): a network retry of the SAME decision reuses it and
// is a no-op replay server-side; a different verb on the same request is a distinct key. Changing
// the reject reason under the same key is caught by the backend fingerprint as a conflict, which is
// the intended optimistic-concurrency behaviour, not a silent overwrite.
function idempotencyKey(requestId: string, verb: "approve" | "reject"): string {
  return `approvals-decide:${verb}:${requestId}`;
}

export async function approveApprovalAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const requestId = String(formData.get("request_id") ?? "").trim();
  if (!requestId) {
    redirect(withFeedback(url, "error", "missing_request_id"));
  }

  const result = await decideAdminWebApproval({
    requestId,
    approve: true,
    reason: "",
    idempotencyKey: idempotencyKey(requestId, "approve"),
  });
  revalidateApprovalViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", "approved"));
}

export async function rejectApprovalAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const requestId = String(formData.get("request_id") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  if (!requestId || !reason) {
    redirect(withFeedback(url, "error", !requestId ? "missing_request_id" : "missing_reason"));
  }

  const result = await decideAdminWebApproval({
    requestId,
    approve: false,
    reason,
    idempotencyKey: idempotencyKey(requestId, "reject"),
  });
  revalidateApprovalViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", "rejected"));
}
