"use server";

import { revalidatePath } from "next/cache";
import {
  addProtocolRule,
  createProtocolDefinition,
  createProtocolVersion,
  previewVaccinationImpact,
  publishProtocolVersion,
  type ImpactPreviewInput,
  type ImpactPreviewResult,
} from "@/lib/api/server";

export interface DoseRow {
  doseCode: string;
  sequence: number;
  triggerType: string;
  offsetDays: number;
  dueWindowDays: number;
  minGapDays: number;
  repeat: string;
  catchUp: string;
}

export interface DraftInput {
  code: string;
  name: string;
  scopeType: string;
  scopeId: string;
  effectiveFrom: string; // yyyy-mm-dd
  eligibility: { stage: string; sex: string; breed: string; parkId: string };
  source: { sourceSystem: string; sourceRef: string; reviewStatus: string; approvedBy: string };
  doses: DoseRow[];
}

export interface ActionResult {
  ok: boolean;
  message: string;
  code?: string;
  versionId?: string;
}

// runImpactPreview computes the live impact via the backend (real inventory/eligibility math).
export async function runImpactPreview(
  input: ImpactPreviewInput,
): Promise<{ ok: boolean; data?: ImpactPreviewResult; message?: string }> {
  const res = await previewVaccinationImpact(input);
  if (!res.ok) return { ok: false, message: res.error.message ?? "impact preview failed" };
  return { ok: true, data: res.data };
}

// saveDraft creates the protocol definition + a DRAFT version (rule_dsl carries eligibility + the
// source/review block that the publish gate validates) + one rule per dose row. Draft never
// generates live obligations.
export async function saveDraft(input: DraftInput): Promise<ActionResult> {
  if (!input.code || !input.name) return { ok: false, message: "code and name are required" };
  if (input.doses.length === 0) return { ok: false, message: "add at least one dose row" };

  const def = await createProtocolDefinition({ code: input.code, name: input.name, category: "vaccination" });
  if (!def.ok) return { ok: false, message: def.error.message ?? "create definition failed", code: def.error.code };

  const ruleDsl = {
    eligibility: {
      stage: input.eligibility.stage,
      sex: input.eligibility.sex,
      breed: input.eligibility.breed,
    },
    source: {
      source_system: input.source.sourceSystem,
      source_ref: input.source.sourceRef,
      review_status: input.source.reviewStatus,
      approved_by: input.source.approvedBy,
    },
  };
  const version = await createProtocolVersion(def.data.protocol_id, {
    scope_type: input.scopeType,
    scope_id: input.scopeType === "park" ? input.scopeId : undefined,
    version: 1,
    effective_from: new Date(`${input.effectiveFrom}T00:00:00Z`).toISOString(),
    rule_dsl: ruleDsl,
    proof_policy: {},
  });
  if (!version.ok) return { ok: false, message: version.error.message ?? "create version failed", code: version.error.code };

  for (const d of input.doses) {
    const rule = await addProtocolRule(version.data.protocol_version_id, {
      dose_code: d.doseCode,
      sequence: d.sequence,
      trigger_type: d.triggerType,
      offset_days: d.offsetDays,
      due_window_days: d.dueWindowDays,
      min_gap_days: d.minGapDays,
      repeat: d.repeat,
      catch_up: d.catchUp,
      eligibility_json: {},
      proof_policy: {},
      sort_order: d.sequence,
    });
    if (!rule.ok) return { ok: false, message: rule.error.message ?? "add rule failed", code: rule.error.code };
  }

  revalidatePath("/vaccination/config");
  return { ok: true, message: "draft saved", versionId: version.data.protocol_version_id };
}

// publishVersion attempts to publish through the source-backed gate. A not-source-backed draft is
// rejected (422 not_publishable) — the message explains exactly which source field is missing.
export async function publishVersion(versionId: string): Promise<ActionResult> {
  if (!versionId) return { ok: false, message: "save the draft first" };
  const res = await publishProtocolVersion(versionId);
  if (!res.ok) {
    return { ok: false, message: res.error.message ?? "publish failed", code: res.error.code };
  }
  revalidatePath("/vaccination");
  revalidatePath("/vaccination/config");
  return { ok: true, message: "published — source-backed; obligations now generate from this version" };
}
