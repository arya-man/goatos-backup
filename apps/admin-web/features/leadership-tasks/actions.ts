"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import {
  changeLeadershipTaskStatus,
  editLeadershipTask,
  raiseLeadershipTask,
  setLeadershipTaskComment,
  uploadLeadershipTaskAttachment,
} from "@/lib/api/server";
import { farmDeadlineToRFC3339 } from "./deadline";
import { TASK_PARAM } from "./params";
import { safeTaskReturnTo, TASKS_PATHNAME } from "./task-url";

const PATHNAME = TASKS_PATHNAME;

function redirectTarget(formData: FormData): URL {
  // The allowlist lives in task-url.ts and is unit-tested: it used to be a bare
  // startsWith("/tasks"), which accepted the /tasks-preview FIXTURE host as the landing page for
  // a live write.
  return new URL(
    safeTaskReturnTo(
      formData.get("return_to") === null
        ? undefined
        : String(formData.get("return_to")),
    ),
    "https://admin.mesha.local",
  );
}

/**
 * Stamps the outcome on the redirect URL for the page's banner to render.
 *
 * `task_status` / `task_code`, NOT `lt_status` / `lt_code`:
 * `features/vaccination-live-tracker/params.ts` already owns `lt_status`, so the old names meant a
 * task write could hand the live tracker a value it reads as its own filter.
 */
function withFeedback(
  url: URL,
  status: "success" | "error",
  code: string,
): string {
  url.searchParams.set(TASK_PARAM.feedbackStatus, status);
  url.searchParams.set(TASK_PARAM.feedbackCode, code);
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
  /**
   * The people the writer actually PICKED in the composer, as ids -- the server does not read
   * "@Ravi" out of the prose, because two active people can share a display name. The field is a
   * comma-separated hidden input, so this is the whole parse. Every id is re-validated
   * server-side under the task's row lock (403 `mention_not_visible`, 400 `invalid_mention`,
   * 400 `too_many_mentions`), so nothing here is an authority; an older page that posts no field
   * at all sends no mentions, which is the behaviour before the composer existed.
   */
  const mentions = String(formData.get("mention_user_ids") ?? "")
    .split(",")
    .map((id) => id.trim())
    .filter(Boolean)
    .map((userID) => ({ user_id: userID }));

  if (!taskID || !comment) {
    redirect(withFeedback(url, "error", "missing_note"));
  }
  if (idempotencyKey.length < 8 || idempotencyKey.length > 200) {
    redirect(withFeedback(url, "error", "invalid_idempotency_key"));
  }

  /**
   * `mentions` is declared on `LeadershipTaskCommentRequest` in
   * `contracts/openapi/app-api.yaml` and is present in the generated client, but
   * `setLeadershipTaskComment`'s own body parameter in `lib/api/server.ts` still reads
   * `{ comment: string }` -- that file is owned by another agent this round. A typed local is
   * assignable to the narrower parameter and is serialised whole, so the field does reach the
   * endpoint; widening that signature to the generated request type is the follow-up.
   */
  const body: { comment: string; mentions?: { user_id: string }[] } = mentions.length
    ? { comment, mentions }
    : { comment };
  const result = await setLeadershipTaskComment(taskID, body, idempotencyKey);
  revalidatePath(PATHNAME);
  if (!result.ok) {
    redirect(
      withFeedback(url, "error", result.error.code ?? result.error.kind),
    );
  }
  redirect(withFeedback(url, "success", "note_added"));
}

/**
 * The EDIT command — the one the web desk never had.
 *
 * POST /app/leadership-tasks/{task_id}/edit has existed all along with no web client, which is
 * exactly what "task editing is not working" meant. The server owns every rule: raiser-only,
 * open/in_progress only (409 `task_closed`), the `row_version` fence (409 `version_conflict`),
 * and an ABSENT `deadline_at` keeps the stored deadline. So `deadline_at` is omitted from the
 * body when the form leaves it blank rather than sent empty — sending "" would be a malformed
 * instant, and sending the rendered value back would silently re-stamp a deadline nobody touched.
 *
 * `attachments` is a FULL replacement list (max 12): the form re-posts the refs the reader kept
 * plus whatever they added, and anything they removed is simply absent.
 */
export async function editLeadershipTaskAction(
  formData: FormData,
): Promise<void> {
  const url = redirectTarget(formData);
  const taskID = String(formData.get("task_id") ?? "").trim();
  const title = String(formData.get("title") ?? "").trim();
  const body = String(formData.get("body") ?? "").trim();
  const rowVersion = Number.parseInt(
    String(formData.get("row_version") ?? ""),
    10,
  );
  const idempotencyKey = String(formData.get("idempotency_key") ?? "").trim();
  const deadlineRaw = String(formData.get("deadline_at") ?? "").trim();
  const deadlineAt = farmDeadlineToRFC3339(deadlineRaw);

  if (!taskID || !title || !Number.isFinite(rowVersion)) {
    redirect(withFeedback(url, "error", !title ? "missing_title" : "invalid_edit"));
  }
  if (deadlineRaw && !deadlineAt) {
    redirect(withFeedback(url, "error", "invalid_deadline"));
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

  const result = await editLeadershipTask(
    taskID,
    {
      title,
      body,
      row_version: rowVersion,
      // Omitted, not blank: absence is the contract's "keep the stored deadline".
      ...(deadlineAt ? { deadline_at: deadlineAt } : {}),
      attachments: [...existingRefs, ...uploads.refs],
    },
    idempotencyKey,
  );

  revalidatePath(PATHNAME);
  if (!result.ok) {
    redirect(
      withFeedback(url, "error", result.error.code ?? result.error.kind),
    );
  }
  redirect(withFeedback(url, "success", "task_edited"));
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

