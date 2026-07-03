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
import {
  buildProtocolRuleRows,
  buildProofPolicy,
  buildRuleDsl,
  parseScope,
  ruleInputForVaccinationMatrixRow,
  type RuleInput,
  type VaccinationMatrixRow,
} from "./rule-dsl";

export interface ActionResult {
  ok: boolean;
  message: string;
  code?: string;
  versionId?: string;
  versionIds?: string[];
}

// runImpactPreview computes the live impact via the backend (real inventory/eligibility math). The
// impact-preview endpoint is vaccination-specific today; other categories add their own preview path
// as their domain lands.
export async function runImpactPreview(
  input: ImpactPreviewInput,
): Promise<{ ok: boolean; data?: ImpactPreviewResult; message?: string }> {
  const res = await previewVaccinationImpact(input);
  if (!res.ok)
    return { ok: false, message: res.error.message ?? "impact preview failed" };
  return { ok: true, data: res.data };
}

// saveDraft persists the authored rule as a protocol_definitions row + a DRAFT protocol_versions row
// whose rule_dsl is the full canonical ruleset (eligibility, defer states, policies, escalation,
// and the schedule[] array), then one protocol_rules row per dose/phase. The category is generic —
// this single action authors any protocol category. Draft never generates live work.
export async function saveDraft(input: RuleInput): Promise<ActionResult> {
  if (!input.category) return { ok: false, message: "category is required" };
  if (!input.code || !input.name)
    return { ok: false, message: "code and name are required" };
  const protocolRows = buildProtocolRuleRows(input);
  if (protocolRows.length === 0)
    return { ok: false, message: "add at least one rule row" };

  const def = await createProtocolDefinition(
    { code: input.code, name: input.name, category: input.category },
    stableMutationKey("protocol-definition", {
      category: input.category,
      code: input.code,
      name: input.name,
    }),
  );
  if (!def.ok)
    return {
      ok: false,
      message: def.error.message ?? "create definition failed",
      code: def.error.code,
    };

  const ruleDsl = buildRuleDsl(input);
  const proofPolicy = buildProofPolicy(input);
  const { type: scopeType, id: scopeId } = parseScope(input.scope);
  const effectiveFrom = new Date(
    `${input.effectiveFrom || new Date().toISOString().slice(0, 10)}T00:00:00Z`,
  ).toISOString();
  // Version-level sop_version_id (real published SOP UUID) + non-empty proof_policy are required by the
  // backend executable-contract gate (publish.go ValidateExecutionContract). Passing them here lets
  // a complete draft publish instead of failing the execution-contract check.
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
    stableMutationKey("protocol-version", {
      protocolId: def.data.protocol_id,
      ...versionBody,
    }),
  );
  if (!version.ok)
    return {
      ok: false,
      message: version.error.message ?? "create version failed",
      code: version.error.code,
    };

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
      eligibility_json:
        typeof ruleDsl.eligibility === "object" && ruleDsl.eligibility !== null
          ? ruleDsl.eligibility
          : {},
      sort_order: d.sortOrder,
    };
    const rule = await addProtocolRule(
      version.data.protocol_version_id,
      ruleBody,
      stableMutationKey("protocol-rule", {
        versionId: version.data.protocol_version_id,
        ...ruleBody,
      }),
    );
    if (!rule.ok)
      return {
        ok: false,
        message: rule.error.message ?? "add rule failed",
        code: rule.error.code,
      };
  }

  revalidatePath("/config");
  return {
    ok: true,
    message: `draft saved - ${protocolRows.length} rule rows - no live obligations`,
    versionId: version.data.protocol_version_id,
  };
}

export async function saveDraftBatch(
  input: RuleInput,
  matrixRows: VaccinationMatrixRow[],
): Promise<ActionResult> {
  if (input.category !== "vaccination") return saveDraft(input);
  const validationError = validateVaccinationMatrixRows(input, matrixRows);
  if (validationError) return { ok: false, message: validationError };
  const rows = normalizeVaccinationMatrixRows(input, matrixRows);

  const versionIds: string[] = [];
  for (let i = 0; i < rows.length; i += 1) {
    const rowInput = ruleInputForVaccinationMatrixRow(
      input,
      rows[i],
      i,
      rows.length,
    );
    const result = await saveDraft(rowInput);
    if (!result.ok || !result.versionId) {
      return {
        ok: false,
        message: `matrix row ${i + 1} (${rows[i].vaccine.code || rows[i].vaccine.name || "unnamed"}) failed: ${result.message}`,
        code: result.code,
        versionIds,
      };
    }
    versionIds.push(result.versionId);
  }
  revalidatePath("/config");
  return {
    ok: true,
    message: `${versionIds.length} matrix drafts saved - no live obligations`,
    versionId: versionIds[0],
    versionIds,
  };
}

// publishVersion attempts to publish through the backend executable-contract gate. The protocol API
// remains authoritative for the final not_publishable decision.
export async function publishVersion(versionId: string): Promise<ActionResult> {
  if (!versionId) return { ok: false, message: "save the draft first" };
  const res = await publishProtocolVersion(
    versionId,
    stableMutationKey("protocol-publish", { versionId }),
  );
  if (!res.ok)
    return {
      ok: false,
      message: res.error.message ?? "publish failed",
      code: res.error.code,
    };
  // A publish generates obligations, which surface across every process-integrity screen.
  for (const p of [
    "/config",
    "/action-center",
    "/vaccination",
    "/protocol-adherence",
    "/workflows",
    "/",
  ]) {
    revalidatePath(p);
  }
  return {
    ok: true,
    message:
      "published - immutable; obligations now generate from this version",
  };
}

export async function publishVersions(
  versionIds: string[],
): Promise<ActionResult> {
  const ids = Array.from(
    new Set(versionIds.map((id) => id.trim()).filter(Boolean)),
  );
  if (ids.length === 0) return { ok: false, message: "save the draft first" };
  const published: string[] = [];
  for (const versionId of ids) {
    const result = await publishVersion(versionId);
    if (!result.ok) {
      return {
        ok: false,
        message: `publish failed after ${published.length}/${ids.length} matrix rows: ${result.message}`,
        code: result.code,
        versionIds: published,
      };
    }
    published.push(versionId);
  }
  return {
    ok: true,
    message: `${published.length} matrix rows published - obligations now generate per eligible breed/stage combo`,
    versionId: published[0],
    versionIds: published,
  };
}

function normalizeVaccinationMatrixRows(
  input: RuleInput,
  matrixRows: VaccinationMatrixRow[],
): VaccinationMatrixRow[] {
  if (input.category !== "vaccination") return [];
  const rows =
    matrixRows.length > 0
      ? matrixRows
      : [
          {
            id: "current",
            vaccine: input.vaccine,
            species: input.eligibility.species,
            stage: input.eligibility.stage,
            sex: input.eligibility.sex,
            breed: input.eligibility.breed,
            doses: input.doses,
          },
        ];
  return rows.map((row, index) => ({
    id: row.id || `row-${index + 1}`,
    vaccine: {
      code: row.vaccine.code.trim(),
      name: row.vaccine.name.trim(),
      type: row.vaccine.type,
      pathogenClass: row.vaccine.pathogenClass,
      courseType: row.vaccine.courseType,
      inventoryItemId: row.vaccine.inventoryItemId.trim(),
      manufacturer: row.vaccine.manufacturer.trim(),
      disease: row.vaccine.disease.trim(),
      compatibilityGroup: row.vaccine.compatibilityGroup.trim(),
    },
    species: row.species || input.eligibility.species,
    stage: row.stage || input.eligibility.stage,
    sex: row.sex || input.eligibility.sex,
    breed: row.breed || input.eligibility.breed,
    doses: row.doses !== undefined ? row.doses : input.doses,
  }));
}

function validateVaccinationMatrixRows(
  input: RuleInput,
  matrixRows: VaccinationMatrixRow[],
): string {
  const rows =
    matrixRows.length > 0
      ? matrixRows
      : [
          {
            id: "current",
            vaccine: input.vaccine,
            species: input.eligibility.species,
            stage: input.eligibility.stage,
            sex: input.eligibility.sex,
            breed: input.eligibility.breed,
            doses: input.doses,
          },
        ];
  for (let i = 0; i < rows.length; i += 1) {
    const row = rows[i];
    const rowLabel = `matrix row ${i + 1}`;
    const vaccineCode = row.vaccine.code.trim();
    const vaccineName = row.vaccine.name.trim();
    if (!vaccineCode) return `${rowLabel}: vaccine code is required`;
    if (!vaccineName) return `${rowLabel}: vaccine name is required`;
    const doseRows = row.doses !== undefined ? row.doses : input.doses;
    if (doseRows.length === 0) {
      return `${rowLabel} (${vaccineCode || vaccineName || "unnamed"}): add at least one dose row or use Copy selected row`;
    }
  }
  return "";
}

function stableMutationKey(scope: string, payload: unknown): string {
  const digest = createHash("sha256")
    .update(stableStringify(payload))
    .digest("hex");
  return `${scope}-${digest}`;
}

function stableStringify(value: unknown): string {
  if (typeof value === "undefined") return "undefined";
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value))
    return `[${value.map((item) => stableStringify(item)).join(",")}]`;
  const obj = value as Record<string, unknown>;
  return `{${Object.keys(obj)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableStringify(obj[key])}`)
    .join(",")}}`;
}
