"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import {
  changeLeadershipTaskStatus,
  raiseLeadershipTask,
  setLeadershipTaskComment,
  uploadLeadershipTaskAttachment,
} from "@/lib/api/server";
import { farmDeadlineToRFC3339 } from "./deadline";

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
  const deadlineAt = farmDeadlineToRFC3339(
    String(formData.get("deadline_at") ?? ""),
  );

  if (!title || !assigneeUserID) {
    redirect(
      withFeedback(url, "error", !title ? "missing_title" : "missing_assignee"),
    );
  }
  if (!deadlineAt) {
    redirect(withFeedback(url, "error", "missing_deadline"));
  }
  if (idempotencyKey.length < 8 || idempotencyKey.length > 200) {
    redirect(withFeedback(url, "error", "invalid_idempotency_key"));
  }

  const existingRefs = attachmentRefs(formData);
  const files = attachmentFiles(formData);
  if (existingRefs.length + files.length > 12) {
    redirect(withFeedback(url, "error", "too_many_attachments"));
  }

  const uploads = await uploadedAttachmentRefs(files, idempotencyKey);
  if (!uploads.ok) {
    redirect(withFeedback(url, "error", uploads.error));
  }

  const result = await raiseLeadershipTask(
    {
      title,
      body,
      assignee_user_id: assigneeUserID,
      deadline_at: deadlineAt,
      attachments: [...uploads.refs, ...existingRefs],
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

export async function changeLeadershipTaskStatusAction(
  formData: FormData,
): Promise<void> {
  const url = redirectTarget(formData);
  const taskID = String(formData.get("task_id") ?? "").trim();
  const status = String(formData.get("status") ?? "").trim();
  const rowVersion = Number.parseInt(
    String(formData.get("row_version") ?? ""),
    10,
  );
  const idempotencyKey = String(formData.get("idempotency_key") ?? "").trim();

  if (!taskID || !status || !Number.isFinite(rowVersion)) {
    redirect(withFeedback(url, "error", "invalid_status_change"));
  }
  if (idempotencyKey.length < 8 || idempotencyKey.length > 200) {
    redirect(withFeedback(url, "error", "invalid_idempotency_key"));
  }

  const result = await changeLeadershipTaskStatus(
    taskID,
    { status, row_version: rowVersion },
    idempotencyKey,
  );
  revalidatePath(PATHNAME);
  if (!result.ok) {
    redirect(
      withFeedback(url, "error", result.error.code ?? result.error.kind),
    );
  }
  redirect(withFeedback(url, "success", "task_updated"));
}

export async function setLeadershipTaskCommentAction(
  formData: FormData,
): Promise<void> {
  const url = redirectTarget(formData);
  const taskID = String(formData.get("task_id") ?? "").trim();
  const comment = String(formData.get("comment") ?? "").trim();
  const idempotencyKey = String(formData.get("idempotency_key") ?? "").trim();

  if (!taskID || !comment) {
    redirect(withFeedback(url, "error", "missing_note"));
  }
  if (idempotencyKey.length < 8 || idempotencyKey.length > 200) {
    redirect(withFeedback(url, "error", "invalid_idempotency_key"));
  }

  const result = await setLeadershipTaskComment(
    taskID,
    { comment },
    idempotencyKey,
  );
  revalidatePath(PATHNAME);
  if (!result.ok) {
    redirect(
      withFeedback(url, "error", result.error.code ?? result.error.kind),
    );
  }
  redirect(withFeedback(url, "success", "note_added"));
}

async function uploadedAttachmentRefs(
  files: File[],
  idempotencyKey: string,
): Promise<
  | {
      ok: true;
      refs: Array<{ proof_id: string; kind: string; file_name?: string }>;
    }
  | { ok: false; error: string }
> {
  const uploads = await Promise.all(
    files.map((file, i) =>
      uploadLeadershipTaskAttachment(file, `${idempotencyKey}:attachment:${i}`),
    ),
  );
  const refs: Array<{ proof_id: string; kind: string; file_name?: string }> = [];
  for (const upload of uploads) {
    if (!upload.ok) {
      return { ok: false, error: upload.error.code ?? upload.error.kind };
    }
    refs.push({
      proof_id: upload.data.proof_id,
      kind: upload.data.kind,
      file_name: upload.data.file_name,
    });
  }
  return { ok: true, refs };
}

function attachmentFiles(formData: FormData): File[] {
  return formData
    .getAll("attachment_file")
    .filter((value): value is File => value instanceof File && value.size > 0);
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
  return refs;
}

