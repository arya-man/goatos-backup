"use server";

import { actionErrorMessage, actionRedirect, requiredString } from "@/lib/action-helpers";
import { createMortalitySyncRun, type CreateMortalitySyncRunRequest } from "@/lib/api/server";

export async function createMortalitySyncRunAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: CreateMortalitySyncRunRequest = {
      dry_run: formData.get("mode") !== "execute",
    };
    const result = await createMortalitySyncRun(body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Mortality sync ${result.data.sync_run_id.slice(0, 8)} ${result.data.status}; ${result.data.events_upserted.toLocaleString("en-IN")} events upserted.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to start Mortality sync.";
  }
  actionRedirect(formData, status, message);
}
