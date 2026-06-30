"use server";

import { createHash } from "crypto";
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
import { buildProtocolRuleRows, buildProofPolicy, buildRuleDsl, parseScope, type RuleInput } from "./rule-dsl";

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

  const def = await createProtocolDefinition(
    { code: input.code, name: input.name, category: input.category },
    stableMutationKey("protocol-definition", { category: input.category, code: input.code, name: input.name }),
  );
  if (!def.ok) return { ok: false, message: def.error.message ?? "create definition failed", code: def.error.code };

  const ruleDsl = buildRuleDsl(input);
  const proofPolicy = buildProofPolicy(input);
  const { type: scopeType, id: scopeId } = parseScope(input.scope);
  const effectiveFrom = new Date(`${input.effectiveFrom || new Date().toISOString().slice(0, 10)}T00:00:00Z`).toISOString();
  // Version-level sop_version_id (real published SOP UUID) + non-empty proof_policy are required by the
  // backend publish gate (publish.go ValidateExecutionContract). Passing them here lets an
  // approved, source-backed draft actually publish instead of failing the execution-contract check.
  const versionBody = {
    scope_type: scopeType,
    scope_id: scopeId ?? undefined,
    version: 1,
    effective_from: effectiveFrom,
    rule_dsl: ruleDsl,
    proof_policy: proofPolicy,
    sop_version_id: input.sopVersionId || undefined,
  };
  const version = await createProtocolVersion(
    def.data.protocol_id,
    versionBody,
    stableMutationKey("protocol-version", { protocolId: def.data.protocol_id, ...versionBody }),
  );
  if (!version.ok) return { ok: false, message: version.error.message ?? "create version failed", code: version.error.code };

  for (const d of protocolRows) {
    // Note: AddProtocolRuleRequest has no sop_version field — the executable SOP is bound at the version
    // level (sop_version_id above), not per rule row. The per-dose SOP label lives in rule_dsl only.
    const ruleBody = {
      dose_code: d.doseCode,
      sequence: d.sortOrder,
      trigger_type: d.trigger,
      offset_days: Number(d.offsetDays) || 0,
      due_window_days: Number(d.dueWindowDays) || 0,
      min_gap_days: Number(d.minGapDays) || 0,
      repeat: d.repeat,
      repeat_until_after_age: d.repeatUntilAfterAge,
      catch_up: d.catchUp,
      proof_policy: d.proofPolicy,
      eligibility_json: typeof ruleDsl.eligibility === "object" && ruleDsl.eligibility !== null ? ruleDsl.eligibility : {},
      sort_order: d.sortOrder,
    };
    const rule = await addProtocolRule(
      version.data.protocol_version_id,
      ruleBody,
      stableMutationKey("protocol-rule", { versionId: version.data.protocol_version_id, ...ruleBody }),
    );
    if (!rule.ok) return { ok: false, message: rule.error.message ?? "add rule failed", code: rule.error.code };
  }

  revalidatePath("/config");
  return { ok: true, message: `draft saved - ${protocolRows.length} rule rows - no live obligations`, versionId: version.data.protocol_version_id };
}

// publishVersion attempts to publish through the backend source-backed gate. The form may show a
// pre-submit disabled reason from backend contract metadata, but this action still treats the
// protocol API as authoritative for the final not_publishable decision.
export async function publishVersion(versionId: string): Promise<ActionResult> {
  if (!versionId) return { ok: false, message: "save the draft first" };
  const res = await publishProtocolVersion(versionId, stableMutationKey("protocol-publish", { versionId }));
  if (!res.ok) return { ok: false, message: res.error.message ?? "publish failed", code: res.error.code };
  // A publish generates obligations, which surface across every process-integrity screen.
  for (const p of ["/config", "/action-center", "/vaccination", "/protocol-adherence", "/workflows", "/"]) {
    revalidatePath(p);
  }
  return { ok: true, message: "published — immutable · source-backed; obligations now generate from this version" };
}

function stableMutationKey(scope: string, payload: unknown): string {
  const digest = createHash("sha256").update(stableStringify(payload)).digest("hex");
  return `${scope}-${digest}`;
}

function stableStringify(value: unknown): string {
  if (typeof value === "undefined") return "undefined";
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map((item) => stableStringify(item)).join(",")}]`;
  const obj = value as Record<string, unknown>;
  return `{${Object.keys(obj)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableStringify(obj[key])}`)
    .join(",")}}`;
}
