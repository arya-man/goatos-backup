"use server";

import { randomUUID } from "crypto";
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

  if (!title || !assigneeUserID) {
    redirect(
      withFeedback(url, "error", !title ? "missing_title" : "missing_assignee"),
    );
  }

  const result = await raiseLeadershipTask(
    {
      title,
      body,
      assignee_user_id: assigneeUserID,
      attachments: [],
    },
    `admin-web-leadership-task:${randomUUID()}`,
  );

  revalidatePath(PATHNAME);
  if (!result.ok) {
    redirect(
      withFeedback(url, "error", result.error.code ?? result.error.kind),
    );
  }
  redirect(withFeedback(url, "success", "task_raised"));
}
