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

import type { ProtocolConfigItem } from "@/lib/api/server";

import type { EditorPlan } from "./editor-model";
import { toRuleDsl } from "./editor-model";
import {
  createProtocolVersion,
  discardProtocolVersion,
  replaceProtocolDraftVersion,
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
  // A dropped connection surfaces as a TypeError from fetch ("fetch failed",
  // ECONNREFUSED, ENOTFOUND). Those are true but useless on screen, and this
  // function exists precisely to keep transport noise off it, so they are
  // reported as what they mean: the server could not be reached.
  if (isUnreachable(detail)) {
    return { ok: false, error: `${prefix}: the server could not be reached. Nothing was changed — try again.` };
  }
  const message =
    typeof detail === "string"
      ? detail
      : detail && typeof detail === "object" && "message" in detail
        ? String((detail as { message: unknown }).message)
        : "unexpected error";
  return { ok: false, error: `${prefix}: ${message}` };
}

function isUnreachable(detail: unknown): boolean {
  const text =
    typeof detail === "string"
      ? detail
      : detail && typeof detail === "object" && "message" in detail
        ? String((detail as { message: unknown }).message)
        : "";
  return /fetch failed|ECONNREFUSED|ENOTFOUND|EAI_AGAIN|socket hang up|network|timeout/i.test(text);
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

  // ONE draft at a time. If a draft already exists, this returns it instead of
  // making a second: a plan being worked on is a single thing, and two competing
  // drafts have no meaning -- whichever was published second would silently
  // discard the other's edits.
  //
  // The list screen hides this action while a draft exists, but that is not the
  // guard: a tab loaded BEFORE the draft was created still shows the button, and
  // pressing it created a second draft (reproduced: V2 and V3 side by side). The
  // check belongs here, where every caller passes.
  const existing = configs.data.items?.find((item) => item.status === "draft");
  if (existing) {
    revalidatePath(PLAN_ROUTE);
    return { ok: true, versionId: existing.protocol_version_id };
  }

  const live = configs.data.items?.find((item) => item.status === "published");
  if (!live) {
    return { ok: false, error: "There is no published vaccination plan to copy from." };
  }

  const current = await getProtocolVersion(live.protocol_version_id);
  if (!current.ok) return failure("could not read the live plan", current.error);

  const created = await createProtocolVersion(live.protocol_id, {
    scope_type: live.scope_type ?? "tenant",
    scope_id: live.scope_id || undefined,
    version_label: nextVersionLabel(configs.data.items ?? []),
    // Publishing is immediate: a future effective_from cannot be closed later,
    // because effective_to is immutable on a published row, which would leave a
    // window with no effective plan. See ADR "Open" item 2.
    effective_from: today(),
    rule_dsl: current.data.rule_dsl,
    proof_policy: current.data.proof_policy,
    sop_version_id: current.data.sop_version_id || undefined,
  });
  if (!created.ok) {
    // The database refused a SECOND draft for this plan. The check above already looked,
    // but a read-then-write check cannot stop two tabs racing -- which is exactly why the
    // invariant lives in the database. Losing that race is not an error to show anyone:
    // the draft they wanted exists, so open it.
    if (isDraftConflict(created.error)) {
      const after = await listProtocolConfigs(CATEGORY);
      const draft = after.ok ? after.data.items?.find((item) => item.status === "draft") : undefined;
      if (draft) {
        revalidatePath(PLAN_ROUTE);
        return { ok: true, versionId: draft.protocol_version_id };
      }
    }
    return failure("could not start a new version", created.error);
  }

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
/**
 * Save the draft.
 *
 * A draft version's rules cannot be updated in place -- protocol_rules may only
 * be attached while a version is a draft, and the row itself carries a
 * row_version -- so a save creates the next draft carrying the edited document
 * and returns its id. The caller must use the RETURNED id afterwards; the one it
 * held is stale.
 */
export async function saveDraftPlan(
  draftVersionId: string,
  plan: EditorPlan,
  originalRuleDsl: unknown,
): Promise<PlanActionResult> {
  const existing = await getProtocolVersion(draftVersionId);
  if (!existing.ok) return failure("could not read the draft", existing.error);
  if (existing.data.status !== "draft") {
    return { ok: false, error: "That version is already published and cannot be edited." };
  }

  const configs = await listProtocolConfigs(CATEGORY);
  const label = configs.ok
    ? configs.data.items?.find((i) => i.protocol_version_id === draftVersionId)?.version_label
    : undefined;

  // ONE call, one transaction: the old draft goes and the replacement lands together.
  //
  // This used to create the replacement and then discard the old row. That means both
  // drafts exist in between, which one-draft-per-plan now refuses in the database -- so
  // every save would have failed with a conflict. Reversing the order in the client is no
  // better: a failure after the discard leaves the farm with nothing.
  const saved = await replaceProtocolDraftVersion(draftVersionId, {
    protocol_id: existing.data.protocol_id,
    scope_type: existing.data.scope_type ?? "tenant",
    scope_id: existing.data.scope_id || undefined,
    version_label: label,
    effective_from: today(),
    rule_dsl: toRuleDsl(originalRuleDsl, plan),
    proof_policy: existing.data.proof_policy,
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

/**
 * The next label a PERSON would say, which is not the next row number.
 *
 * `version` is the database's own counter and drifts away from the label: this
 * tenant has version 1 and version 2 BOTH labelled "V1 Real Vaccination", so
 * incrementing the counter produced "V3" for what is only the second plan
 * anyone has ever seen. The label is what the CEO recognises, so the next one
 * is derived from the labels themselves -- highest V-number in use, plus one.
 */
function nextVersionLabel(items: ProtocolConfigItem[]): string {
  let highest = 0;
  for (const item of items) {
    const match = /^v\s*(\d+)/i.exec((item.version_label ?? "").trim());
    if (match) highest = Math.max(highest, Number(match[1]));
  }
  return `V${highest + 1}`;
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

/**
 * Discard a draft.
 *
 * Destroys unpublished authoring work, so the caller must confirm first. The
 * backend refuses anything that is not a draft, so a published plan cannot be
 * removed through this path even if a stale id reaches it.
 */
export async function discardDraft(draftVersionId: string): Promise<PlanActionResult> {
  const discarded = await discardProtocolVersion(draftVersionId);
  if (!discarded.ok) {
    // A 404 means the draft is already gone -- discarded in another tab, or the
    // page was open long enough to go stale. The caller asked for it not to
    // exist, and it does not exist, so this is the outcome they wanted. Report
    // success and let the refresh below correct the stale screen.
    if (isNotFound(discarded.error)) {
      revalidatePath(PLAN_ROUTE);
      return { ok: true };
    }
    return failure("could not discard the draft", discarded.error);
  }
  revalidatePath(PLAN_ROUTE);
  return { ok: true };
}

function isDraftConflict(detail: unknown): boolean {
  if (!detail || typeof detail !== "object") return false;
  const body = (detail as { body?: { code?: unknown } }).body;
  return body?.code === "draft_already_exists";
}

function isNotFound(detail: unknown): boolean {
  if (!detail || typeof detail !== "object") return false;
  const status = (detail as { status?: unknown }).status;
  if (status === 404) return true;
  const body = (detail as { body?: { code?: unknown } }).body;
  return body?.code === "not_found";
}
