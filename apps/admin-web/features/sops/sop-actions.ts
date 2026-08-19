"use server";

import { revalidatePath } from "next/cache";

// The three module SOP pages (SOP split, maintainer decision 2026-08-18) all render /admin/sops
// data, so every SOP mutation revalidates all of them.
const SOP_PAGE_PATHS = ["/vaccination/sops", "/counts/sops", "/feed/sops"];
import {
  createSop,
  createSopVersion,
  dryRunSopVersion,
  publishSopVersion,
  type DryRunResponse,
  type SOPValidationReport,
} from "@/lib/api/server";
import {
  SOP_SLICE_LABEL,
  buildFormDsl,
  buildProofPolicy,
  buildSopCode,
  hasProofField,
  type SopBuilderInput,
} from "./sop-derive";

export interface SaveSopResult {
  ok: boolean;
  message: string;
  code?: string;
  sopId?: string;
  versionId?: string;
  rowVersion?: number;
  report?: SOPValidationReport;
}

// saveSopDraft persists a new SOP definition + a DRAFT version whose form_dsl + proof_policy are built
// from the builder input. Two real calls: POST /admin/sops then POST /admin/sops/{id}/versions. Draft
// never publishes / generates live work; the returned validation_report is the backend's verdict.
export async function saveSopDraft(input: SopBuilderInput): Promise<SaveSopResult> {
  const name = input.name.trim();
  if (!name) return { ok: false, message: "SOP name is required" };
  if (input.steps.length === 0) return { ok: false, message: "add at least one step / question" };
  // Mirror backend hasProofField: proof_policy.required needs a photo/video proof step present.
  if (input.proofRequired && !hasProofField(input)) {
    return { ok: false, message: "Proof is required but no photo/video proof step exists — add one or turn proof off." };
  }

  const code = buildSopCode(input);
  const def = await createSop({ code, name, description: `${SOP_SLICE_LABEL[input.domain]} · ${input.trigger} SOP` });
  if (!def.ok) return { ok: false, message: def.error.message ?? "create SOP failed", code: def.error.code };

  const version = await createSopVersion(def.data.sop.sop_id, {
    version_label: "v1 draft",
    form_dsl: buildFormDsl(input) as unknown as Record<string, unknown>,
    proof_policy: buildProofPolicy(input),
  });
  if (!version.ok) {
    return { ok: false, message: version.error.message ?? "create SOP version failed", code: version.error.code };
  }

  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  const report = version.data.version.validation_report;
  return {
    ok: true,
    message: report?.valid
      ? "Draft saved — form_dsl validated. Dry-run or publish below."
      : "Draft saved — backend flagged validation issues (see report).",
    sopId: def.data.sop.sop_id,
    versionId: version.data.version.sop_version_id,
    rowVersion: version.data.version.row_version,
    report,
  };
}

export async function saveSopVersionDraft(sopId: string, input: SopBuilderInput): Promise<SaveSopResult> {
  if (!sopId) return { ok: false, message: "SOP id is required" };
  const name = input.name.trim();
  if (!name) return { ok: false, message: "SOP name is required" };
  if (input.steps.length === 0) return { ok: false, message: "add at least one step / question" };
  if (input.proofRequired && !hasProofField(input)) {
    return { ok: false, message: "Proof is required but no photo/video proof step exists — add one or turn proof off." };
  }

  const version = await createSopVersion(sopId, {
    version_label: "edited draft",
    form_dsl: buildFormDsl(input) as unknown as Record<string, unknown>,
    proof_policy: buildProofPolicy(input),
  });
  if (!version.ok) {
    return { ok: false, message: version.error.message ?? "create SOP version failed", code: version.error.code };
  }

  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  const report = version.data.version.validation_report;
  return {
    ok: true,
    message: report?.valid
      ? "Edited draft saved — form_dsl validated. Dry-run or publish below."
      : "Edited draft saved — backend flagged validation issues (see report).",
    sopId,
    versionId: version.data.version.sop_version_id,
    rowVersion: version.data.version.row_version,
    report,
  };
}

export interface DryRunActionResult {
  ok: boolean;
  message?: string;
  data?: DryRunResponse;
}

// runDryRun calls the real server-side preview validator with empty answers + no proof — it returns the
// per-field visible/required/blocked states and the workflow path the runner would take. Honest preview.
export async function runDryRun(sopId: string, versionId: string): Promise<DryRunActionResult> {
  if (!sopId || !versionId) return { ok: false, message: "save the draft first" };
  const res = await dryRunSopVersion(sopId, versionId, { answers: {}, proof_refs: [] });
  if (!res.ok) return { ok: false, message: res.error.message ?? "dry-run failed" };
  return { ok: true, data: res.data };
}

export interface PublishSopResult {
  ok: boolean;
  message: string;
  code?: string;
}

// publishSop publishes the immutable version (real RowVersionRequest). No fake publish state — the
// backend gate decides; a rejection surfaces verbatim.
export async function publishSop(sopId: string, versionId: string, rowVersion: number): Promise<PublishSopResult> {
  if (!sopId || !versionId) return { ok: false, message: "save the draft first" };
  const res = await publishSopVersion(sopId, versionId, rowVersion);
  if (!res.ok) return { ok: false, message: res.error.message ?? "publish failed", code: res.error.code };
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  return { ok: true, message: "Published — immutable version; tasks pin to it." };
}
