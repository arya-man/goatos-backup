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
import { buildProtocolRuleRows, buildRuleDsl, parseScope, validatePublish, type RuleInput } from "./rule-dsl";

export interface ActionResult {
  ok: boolean;
  message: string;
  code?: string;
  versionId?: string;
}

// runImpactPreview computes the live impact via the backend (real inventory/eligibility math). The
// impact-preview endpoint is vaccination-specific today; other categories add their own preview path
// as their domain lands.
export async function runImpactPreview(
  input: ImpactPreviewInput,
): Promise<{ ok: boolean; data?: ImpactPreviewResult; message?: string }> {
  const res = await previewVaccinationImpact(input);
  if (!res.ok) return { ok: false, message: res.error.message ?? "impact preview failed" };
  return { ok: true, data: res.data };
}

// saveDraft persists the authored rule as a protocol_definitions row + a DRAFT protocol_versions row
// whose rule_dsl is the full canonical ruleset (eligibility, defer states, policies, escalation,
// source/review, and the schedule[] array), then one protocol_rules row per dose/phase. The category
// is generic — this single action authors any protocol category. Draft never generates live work.
export async function saveDraft(input: RuleInput): Promise<ActionResult> {
  if (!input.category) return { ok: false, message: "category is required" };
  if (!input.code || !input.name) return { ok: false, message: "code and name are required" };
  const protocolRows = buildProtocolRuleRows(input);
  if (protocolRows.length === 0) return { ok: false, message: "add at least one rule row" };

  const def = await createProtocolDefinition({ code: input.code, name: input.name, category: input.category });
  if (!def.ok) return { ok: false, message: def.error.message ?? "create definition failed", code: def.error.code };

  const ruleDsl = buildRuleDsl(input);
  const { type: scopeType, id: scopeId } = parseScope(input.scope);
  const version = await createProtocolVersion(def.data.protocol_id, {
    scope_type: scopeType,
    scope_id: scopeId ?? undefined,
    version: 1,
    effective_from: new Date(`${input.effectiveFrom || new Date().toISOString().slice(0, 10)}T00:00:00Z`).toISOString(),
    rule_dsl: ruleDsl,
    proof_policy: {},
  });
  if (!version.ok) return { ok: false, message: version.error.message ?? "create version failed", code: version.error.code };

  for (const d of protocolRows) {
    const rule = await addProtocolRule(version.data.protocol_version_id, {
      dose_code: d.doseCode,
      sequence: d.sortOrder,
      trigger_type: d.trigger,
      offset_days: Number(d.offsetDays) || 0,
      due_window_days: Number(d.dueWindowDays) || 0,
      min_gap_days: Number(d.minGapDays) || 0,
      repeat: d.repeat,
      repeat_until_after_age: d.repeatUntilAfterAge,
      catch_up: d.catchUp,
      sop_version: d.sopVersion,
      proof_policy: d.proofPolicy,
      eligibility_json: {},
      sort_order: d.sortOrder,
    });
    if (!rule.ok) return { ok: false, message: rule.error.message ?? "add rule failed", code: rule.error.code };
  }

  revalidatePath("/config");
  return { ok: true, message: `draft saved - ${protocolRows.length} rule rows - no live obligations`, versionId: version.data.protocol_version_id };
}

// publishVersion attempts to publish through the source-backed gate. The gate is also enforced
// client-side (validatePublish) and by the backend (422 not_publishable); this re-checks before the
// network call so a not-source-backed draft fails fast with the exact missing field.
export async function publishVersion(versionId: string, source: RuleInput["source"]): Promise<ActionResult> {
  if (!versionId) return { ok: false, message: "save the draft first" };
  const gate = validatePublish(source);
  if (!gate.ok) return { ok: false, message: gate.message ?? "not publishable" };
  const res = await publishProtocolVersion(versionId);
  if (!res.ok) return { ok: false, message: res.error.message ?? "publish failed", code: res.error.code };
  // A publish generates obligations, which surface across every process-integrity screen.
  for (const p of ["/config", "/action-center", "/vaccination", "/protocol-adherence", "/workflows", "/"]) {
    revalidatePath(p);
  }
  return { ok: true, message: "published — immutable · source-backed; obligations now generate from this version" };
}
