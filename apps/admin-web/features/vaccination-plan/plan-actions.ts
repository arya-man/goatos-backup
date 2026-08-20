"use server";

/**
 * Server actions for the Vaccination plan console (Preventive Care).
 *
 * The lifecycle these implement is forced by the database, not chosen here — see
 * docs/preventive-care-vaccination/design/E2E-PUBLISH-SEMANTICS.md §1:
 *
 *   1. a published protocol_version is immutable (only status -> retired is allowed)
 *   2. protocol_rules may only be attached while the version is a draft
 *   3. one published version per (protocol, scope, date range)
 *
 * So a change is never an edit. It is: copy the live plan into a new draft, mutate
 * the copy, publish. Publishing retires the previous version inside the same
 * backend transaction (retirePublishedVaccinationMatrixOverlapsTx).
 */

import { revalidatePath } from "next/cache";

import {
  createProtocolVersion,
  getProtocolVersion,
  listProtocolConfigs,
  publishProtocolVersion,
} from "@/lib/api/server";

export type PlanActionResult =
  | { ok: true; versionId?: string }
  | { ok: false; error: string };

const PLAN_ROUTE = "/vaccination/plan";
const CATEGORY = "vaccination";

/** Human message for a failed API call, never a raw transport error. */
function failure(prefix: string, detail: unknown): PlanActionResult {
  const message =
    typeof detail === "string"
      ? detail
      : detail && typeof detail === "object" && "message" in detail
        ? String((detail as { message: unknown }).message)
        : "unexpected error";
  return { ok: false, error: `${prefix}: ${message}` };
}

/**
 * Start a new version.
 *
 * THE critical rule (MOCK-BEHAVIOUR-SPEC §3.1): this deep-copies the currently
 * live plan. The user changes one field; everything else rides along untouched.
 * There is no backend "copy version" primitive, so the copy happens here: read
 * the published version's rule_dsl and post it verbatim as a new draft.
 *
 * Copying the WHOLE rule_dsl is not a convenience. A new version must carry a
 * complete rule set, because rules cannot be attached to it after publish.
 */
export async function startNewVersion(): Promise<PlanActionResult> {
  const configs = await listProtocolConfigs(CATEGORY);
  if (!configs.ok) return failure("could not read the current plan", configs.error);

  const live = configs.data.items?.find((item) => item.status === "published");
  if (!live) {
    return { ok: false, error: "There is no published vaccination plan to copy from." };
  }

  const current = await getProtocolVersion(live.protocol_version_id);
  if (!current.ok) return failure("could not read the live plan", current.error);

  const created = await createProtocolVersion(live.protocol_id, {
    scope_type: live.scope_type ?? "tenant",
    scope_id: live.scope_id || undefined,
    version_label: nextVersionLabel(live.version),
    // Publishing is immediate: a future effective_from cannot be closed later,
    // because effective_to is immutable on a published row, which would leave a
    // window with no effective plan. See ADR "Open" item 2.
    effective_from: today(),
    rule_dsl: current.data.rule_dsl,
    proof_policy: current.data.proof_policy,
    sop_version_id: current.data.sop_version_id || undefined,
  });
  if (!created.ok) return failure("could not start a new version", created.error);

  revalidatePath(PLAN_ROUTE);
  return { ok: true, versionId: created.data.protocol_version_id };
}

/**
 * Save the draft's rule_dsl.
 *
 * Drafts are mutable, so this replaces the whole document rather than patching —
 * the plan is one coherent bundle and cross-vaccine safety rules must never see a
 * half-applied state.
 */
export async function saveDraftPlan(
  protocolId: string,
  draftVersionId: string,
  versionLabel: string,
  ruleDsl: unknown,
  proofPolicy: unknown,
): Promise<PlanActionResult> {
  const existing = await getProtocolVersion(draftVersionId);
  if (!existing.ok) return failure("could not read the draft", existing.error);
  if (existing.data.status !== "draft") {
    return { ok: false, error: "That version is already published and cannot be edited." };
  }

  const saved = await createProtocolVersion(protocolId, {
    scope_type: existing.data.scope_type ?? "tenant",
    scope_id: existing.data.scope_id || undefined,
    version_label: versionLabel,
    effective_from: today(),
    rule_dsl: ruleDsl,
    proof_policy: proofPolicy,
    sop_version_id: existing.data.sop_version_id || undefined,
  });
  if (!saved.ok) return failure("could not save the draft", saved.error);

  revalidatePath(PLAN_ROUTE);
  return { ok: true, versionId: saved.data.protocol_version_id };
}

/**
 * Publish a draft.
 *
 * The backend retires the previous published version inside this same
 * transaction. There is no separate retire call, and there must not be one — two
 * statements could leave the tenant with zero effective plans between them.
 */
export async function publishPlan(draftVersionId: string): Promise<PlanActionResult> {
  const published = await publishProtocolVersion(draftVersionId);
  if (!published.ok) return failure("could not publish the plan", published.error);

  revalidatePath(PLAN_ROUTE);
  return { ok: true, versionId: draftVersionId };
}

/**
 * Today as a full RFC 3339 timestamp.
 *
 * NOT a bare "YYYY-MM-DD". effective_from decodes into a Go time.Time, which
 * rejects a date-only string -- the whole request then fails as "invalid_json",
 * naming the body rather than the field, so this is worth stating outright.
 */
function today(): string {
  const now = new Date();
  return new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate())).toISOString();
}

function nextVersionLabel(currentVersion: number | undefined): string {
  const next = Number.isFinite(currentVersion) ? Number(currentVersion) + 1 : 1;
  return `V${next}`;
}

/**
 * Read one version's vaccine settings, for the read-only history sheet.
 *
 * Deliberately a server action rather than a page-load fetch: loading every
 * earlier version's document up front would be an unbounded read that grows
 * with the tenant's history, and the reader opens at most one.
 */
export async function readVersionSettings(
  versionId: string,
): Promise<{ ok: true; ruleDsl: unknown } | { ok: false; error: string }> {
  const version = await getProtocolVersion(versionId);
  if (!version.ok) return { ok: false, error: "Those settings could not be loaded." };
  return { ok: true, ruleDsl: version.data.rule_dsl };
}
