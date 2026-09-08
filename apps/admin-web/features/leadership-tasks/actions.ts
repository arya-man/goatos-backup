"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { raiseLeadershipTask } from "@/lib/api/server";

const PATHNAME = "/tasks";

function redirectTarget(formData: FormData): URL {
  const returnTo = String(
    formData.get("return_to") ?? `${PATHNAME}?scope=assigned_by_me`,
  );
  const safe = returnTo.startsWith(PATHNAME)
    ? returnTo
    : `${PATHNAME}?scope=assigned_by_me`;
  return new URL(safe, "https://admin.mesha.local");
}

function withFeedback(
  url: URL,
  status: "success" | "error",
  code: string,
): string {
  url.searchParams.set("lt_status", status);
  url.searchParams.set("lt_code", code);
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

export async function raiseLeadershipTaskAction(
  formData: FormData,
): Promise<void> {
  const url = redirectTarget(formData);
  const title = String(formData.get("title") ?? "").trim();
  const assigneeUserID = String(formData.get("assignee_user_id") ?? "").trim();
  const body = String(formData.get("body") ?? "").trim();
  const idempotencyKey = String(formData.get("idempotency_key") ?? "").trim();

  if (!title || !assigneeUserID) {
    redirect(
      withFeedback(url, "error", !title ? "missing_title" : "missing_assignee"),
    );
  }
  if (idempotencyKey.length < 8 || idempotencyKey.length > 200) {
    redirect(withFeedback(url, "error", "invalid_idempotency_key"));
  }

  const result = await raiseLeadershipTask(
    {
      title,
      body,
      assignee_user_id: assigneeUserID,
      attachments: attachmentRefs(formData),
    },
    idempotencyKey,
  );

  revalidatePath(PATHNAME);
  if (!result.ok) {
    redirect(
      withFeedback(url, "error", result.error.code ?? result.error.kind),
    );
  }
  redirect(withFeedback(url, "success", "task_raised"));
}

function attachmentRefs(
  formData: FormData,
): Array<{ proof_id: string; kind: string; file_name?: string }> {
  const proofIDs = formData
    .getAll("attachment_proof_id")
    .map((value) => String(value).trim());
  const kinds = formData
    .getAll("attachment_kind")
    .map((value) => String(value).trim());
  const fileNames = formData
    .getAll("attachment_file_name")
    .map((value) => String(value).trim());
  const refs: Array<{ proof_id: string; kind: string; file_name?: string }> =
    [];
  for (let i = 0; i < proofIDs.length; i += 1) {
    const proofID = proofIDs[i];
    const kind = kinds[i];
    if (!proofID || !["audio", "video", "photo", "file"].includes(kind)) {
      continue;
    }
    refs.push({
      proof_id: proofID,
      kind,
      file_name: fileNames[i] || undefined,
    });
  }
  return refs.slice(0, 12);
}
