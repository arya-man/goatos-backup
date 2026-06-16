"use server";

import { actionErrorMessage, actionRedirect, requiredString } from "@/lib/action-helpers";
import {
  cancelLegacySyncRun,
  createLegacySyncRun,
  type CreateLegacySyncRunRequest,
  type LegacySyncDomain,
  type LegacySyncMode,
} from "@/lib/api/server";

const modes: LegacySyncMode[] = ["dry_run"];
const domains: LegacySyncDomain[] = ["all", "identity", "lifecycle", "current_location", "active_count"];

export async function createLegacySyncRunAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const mode = requiredMode(formData);
    const domain = requiredDomain(formData);
    const body: CreateLegacySyncRunRequest = {
      mode,
      domain,
      source_window_start: optionalDateTime(formData, "source_window_start"),
      source_window_end: optionalDateTime(formData, "source_window_end"),
    };
    const result = await createLegacySyncRun(body);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      const run = result.data.run;
      message = `Legacy sync ${shortRun(run.sync_run_id)} ${run.status}.`;
      formData.set("return_to", `/legacy-sync?sync_run_id=${encodeURIComponent(run.sync_run_id)}`);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to start legacy sync.";
  }
  actionRedirect(formData, status, message);
}

export async function cancelLegacySyncRunAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const runID = requiredString(formData, "sync_run_id");
    const result = await cancelLegacySyncRun(runID);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Legacy sync ${shortRun(result.data.run.sync_run_id)} is ${result.data.run.status}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to cancel legacy sync.";
  }
  actionRedirect(formData, status, message);
}

function requiredMode(formData: FormData): LegacySyncMode {
  const mode = requiredString(formData, "mode");
  if (!modes.includes(mode as LegacySyncMode)) {
    throw new Error("mode is not supported");
  }
  return mode as LegacySyncMode;
}

function requiredDomain(formData: FormData): LegacySyncDomain {
  const domain = requiredString(formData, "domain");
  if (!domains.includes(domain as LegacySyncDomain)) {
    throw new Error("domain is not supported");
  }
  return domain as LegacySyncDomain;
}

function optionalDateTime(formData: FormData, key: string): string | null {
  const value = formData.get(key);
  if (typeof value !== "string" || value.trim() === "") return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    throw new Error(`${key} must be a valid date and time`);
  }
  return date.toISOString();
}

function shortRun(runID: string): string {
  return runID.slice(0, 8);
}
