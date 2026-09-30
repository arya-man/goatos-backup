"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { decideAdminWebLeave, setLeaveApprovalConfig, type LeaveApprovalConfig } from "@/lib/api/server";

// Leave requests (maintainer decisions 2026-09-10, docs/features/leave-requests/plan.md): the
// park head / HR / CEO decides from /leave; the CEO alone sets who must approve. Both are Server
// Actions -- authenticated through the server config, never a client fetch -- and the decision
// verbs ride the SAME /admin-web/leave/approvals routes the phone's outbox drains through.

const PATHNAME = "/leave";

function redirectTarget(formData: FormData): URL {
  const returnTo = String(formData.get("return_to") ?? PATHNAME);
  const safe = returnTo.startsWith(PATHNAME) ? returnTo : PATHNAME;
  return new URL(safe, "https://admin.mesha.local");
}

function withFeedback(url: URL, status: "success" | "error", code: string): string {
  url.searchParams.set("lv_status", status);
  url.searchParams.set("lv_code", code);
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

// One STABLE key per (request, line, verb): a retry of the same decision replays server-side; the
// backend refuses a second decision on an already-signed line with 409 leave_slot_decided. The web
// only ever signs the HR line (2026-09-30: park heads decide on the phone), so the key names it and
// can never collide with the park-head decision on the same request.
function idempotencyKey(requestId: string, verb: "approve" | "reject"): string {
  return `leave-hr-${verb}:${requestId}`;
}

export async function approveLeaveAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const requestId = String(formData.get("leave_request_id") ?? "").trim();
  if (!requestId) redirect(withFeedback(url, "error", "missing_request_id"));
  const result = await decideAdminWebLeave({ requestId, approve: true, reason: "", idempotencyKey: idempotencyKey(requestId, "approve") });
  revalidatePath(PATHNAME);
  revalidatePath("/people");
  if (!result.ok) redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  redirect(withFeedback(url, "success", "approved"));
}

export async function rejectLeaveAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const requestId = String(formData.get("leave_request_id") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  if (!requestId || !reason) redirect(withFeedback(url, "error", !requestId ? "missing_request_id" : "reason_required"));
  const result = await decideAdminWebLeave({ requestId, approve: false, reason, idempotencyKey: idempotencyKey(requestId, "reject") });
  revalidatePath(PATHNAME);
  revalidatePath("/people");
  if (!result.ok) redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  redirect(withFeedback(url, "success", "rejected"));
}

export type SaveLeaveConfigResult = { ok: true; config: LeaveApprovalConfig } | { ok: false; message: string };

/** Save who must approve leave (CEO only; the route refuses anyone else). */
export async function saveLeaveConfigAction(body: {
  park_head_required: boolean;
  hr_required: boolean;
  row_version: number;
}): Promise<SaveLeaveConfigResult> {
  if (!Number.isInteger(body.row_version) || body.row_version < 0) {
    return { ok: false, message: "Reload the page to see the current settings, then try again." };
  }
  const result = await setLeaveApprovalConfig(body);
  if (!result.ok) {
    return { ok: false, message: result.error.message || "That could not be saved. Reload the page and try again." };
  }
  revalidatePath(PATHNAME);
  return { ok: true, config: result.data.config };
}
