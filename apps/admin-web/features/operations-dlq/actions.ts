"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { discardOutboxDLQ, replayOutboxDLQ } from "@/lib/api/server";

const PATHNAME = "/operations/dlq";

export async function replayDLQAction(formData: FormData) {
  await repairDLQ(formData, "replay");
}

export async function discardDLQAction(formData: FormData) {
  await repairDLQ(formData, "discard");
}

async function repairDLQ(formData: FormData, action: "replay" | "discard") {
  const outboxId = String(formData.get("outbox_id") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  const returnTo = safeReturnTo(String(formData.get("return_to") ?? ""));
  const redirectURL = new URL(returnTo, "https://admin.mesha.local");
  redirectURL.searchParams.set("dlq_id", outboxId);

  if (!outboxId || !reason) {
    redirectURL.searchParams.set("action_status", "failed");
    redirectURL.searchParams.set("action_key", "action.failed");
    redirectURL.searchParams.set("action_code", !outboxId ? "missing_outbox_id" : "missing_reason");
    redirect(redirectPath(redirectURL));
  }

  const result =
    action === "replay"
      ? await replayOutboxDLQ({ outbox_ids: [outboxId], reason })
      : await discardOutboxDLQ({ outbox_ids: [outboxId], reason });

  revalidatePath(PATHNAME);
  revalidatePath("/operations/audit");

  if (!result.ok) {
    redirectURL.searchParams.set("action_status", "failed");
    redirectURL.searchParams.set("action_key", "action.failed");
    redirectURL.searchParams.set("action_code", result.error.code ?? result.error.kind);
    redirect(redirectPath(redirectURL));
  }

  redirectURL.searchParams.set("action_status", "success");
  redirectURL.searchParams.set("action_key", action === "replay" ? "action.replay_success" : "action.discard_success");
  redirectURL.searchParams.set("updated", String(result.data.updated));
  redirect(redirectPath(redirectURL));
}

function safeReturnTo(raw: string): string {
  if (!raw.startsWith(PATHNAME)) return PATHNAME;
  const url = new URL(raw, "https://admin.mesha.local");
  if (url.pathname !== PATHNAME) return PATHNAME;
  url.searchParams.delete("action_status");
  url.searchParams.delete("action_key");
  url.searchParams.delete("action_code");
  url.searchParams.delete("updated");
  return redirectPath(url);
}

function redirectPath(url: URL) {
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}
