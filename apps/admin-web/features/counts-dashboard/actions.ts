"use server";

import { actionErrorMessage, actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
import { createCountsSyncRun, type CreateCountsSyncRunRequest } from "@/lib/api/server";

export async function createCountsSyncRunAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: CreateCountsSyncRunRequest = {
      dry_run: formData.get("mode") !== "execute",
      snapshot_date: optionalString(formData, "snapshot_date") ?? null,
    };
    const result = await createCountsSyncRun(body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Counts sync ${result.data.sync_run_id.slice(0, 8)} ${result.data.status}; ${result.data.projection_rows_written.toLocaleString("en-IN")} rows projected.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to start Counts sync.";
  }
  actionRedirect(formData, status, message);
}
