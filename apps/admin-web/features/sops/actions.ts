"use server";

import {
  createSOP,
  createSOPVersion,
  dryRunSOPVersion,
  publishSOPVersion,
  retireSOPVersion,
  type CreateSOPRequest,
  type CreateSOPVersionRequest,
  type DryRunRequest,
} from "@/lib/api/server";
import { actionErrorMessage } from "@/lib/action-helpers";

export interface ActionResult {
  ok: boolean;
  message: string;
}

export interface CreateSopResult extends ActionResult {
  sopId?: string;
}

export interface CreateVersionInput {
  sopId: string;
  versionLabel: string;
  formDsl: Record<string, unknown>;
  proofPolicy: Record<string, unknown>;
  compatibility: Record<string, unknown>;
  sample: DryRunRequest;
}

export interface CreateVersionResult extends ActionResult {
  sopId?: string;
  sopVersionId?: string;
  versionStatus?: string;
  rowVersion?: number;
  validationValid?: boolean;
  validationErrors?: string[];
  dryRunValid?: boolean;
  dryRunErrors?: string[];
  dryRunPath?: string[];
}

export interface VersionLifecycleResult extends ActionResult {
  status?: string;
  rowVersion?: number;
}

function errMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

export async function createSopAction(input: { code: string; name: string; description?: string }): Promise<CreateSopResult> {
  try {
    const body: CreateSOPRequest = {
      code: input.code.trim(),
      name: input.name.trim(),
      description: input.description?.trim() ?? "",
    };
    const result = await createSOP(body);
    if (!result.ok) return { ok: false, message: actionErrorMessage(result.error) };
    return { ok: true, message: `${result.data.sop.name} created.`, sopId: result.data.sop.sop_id };
  } catch (error) {
    return { ok: false, message: errMessage(error, "Unable to create SOP.") };
  }
}

export async function createVersionAction(input: CreateVersionInput): Promise<CreateVersionResult> {
  try {
    const body: CreateSOPVersionRequest = {
      version_label: input.versionLabel,
      form_dsl: input.formDsl,
      proof_policy: input.proofPolicy,
      compatibility: input.compatibility,
    } as CreateSOPVersionRequest;

    const created = await createSOPVersion(input.sopId, body);
    if (!created.ok) return { ok: false, message: actionErrorMessage(created.error) };

    const version = created.data.version;
    const report = version.validation_report;
    const validationErrors = collectMessages(report?.errors);

    const result: CreateVersionResult = {
      ok: true,
      message: `${version.version_label} draft created.`,
      sopId: input.sopId,
      sopVersionId: version.sop_version_id,
      versionStatus: version.status,
      rowVersion: version.row_version,
      validationValid: report?.valid ?? false,
      validationErrors,
    };

    // Authoritative server dry-run against the freshly created version.
    const dry = await dryRunSOPVersion(input.sopId, version.sop_version_id, input.sample);
    if (dry.ok) {
      result.dryRunValid = dry.data.valid;
      result.dryRunErrors = collectMessages(dry.data.errors);
      result.dryRunPath = Array.isArray(dry.data.workflow_path) ? dry.data.workflow_path : [];
    } else {
      result.dryRunValid = false;
      result.dryRunErrors = [actionErrorMessage(dry.error)];
    }
    return result;
  } catch (error) {
    return { ok: false, message: errMessage(error, "Unable to create version.") };
  }
}

export async function publishVersionAction(input: { sopId: string; sopVersionId: string; rowVersion: number }): Promise<VersionLifecycleResult> {
  try {
    const result = await publishSOPVersion(input.sopId, input.sopVersionId, { row_version: input.rowVersion });
    if (!result.ok) return { ok: false, message: actionErrorMessage(result.error) };
    return { ok: true, message: `${result.data.version.version_label} ${result.data.version.status}.`, status: result.data.version.status, rowVersion: result.data.version.row_version };
  } catch (error) {
    return { ok: false, message: errMessage(error, "Unable to publish version.") };
  }
}

export async function retireVersionAction(input: { sopId: string; sopVersionId: string; rowVersion: number }): Promise<VersionLifecycleResult> {
  try {
    const result = await retireSOPVersion(input.sopId, input.sopVersionId, { row_version: input.rowVersion });
    if (!result.ok) return { ok: false, message: actionErrorMessage(result.error) };
    return { ok: true, message: `${result.data.version.version_label} ${result.data.version.status}.`, status: result.data.version.status, rowVersion: result.data.version.row_version };
  } catch (error) {
    return { ok: false, message: errMessage(error, "Unable to retire version.") };
  }
}

function collectMessages(errors: unknown): string[] {
  if (!Array.isArray(errors)) return [];
  return errors
    .map((e) => {
      if (e && typeof e === "object" && "message" in e) return String((e as { message?: unknown }).message ?? "");
      return typeof e === "string" ? e : "";
    })
    .filter(Boolean);
}
