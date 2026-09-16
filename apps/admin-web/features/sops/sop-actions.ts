"use server";

import { revalidatePath } from "next/cache";

// The module SOP pages (SOP split, maintainer decision 2026-08-18) all render /admin/sops
// data, so every SOP mutation revalidates all of them. Vaccination is no longer among
// them: its SOP surface was absorbed into Preventive Care / Vaccination plan, where the
// proof method is one field on the plan rather than a separate document to author.
//
// Every module SOP route the sidebar serves is listed (review finding on PR 267): a route
// missing here keeps serving the cached library after a publish, so the "Published vN"
// banner and the lit card would point at a card still reading the old version.
const SOP_PAGE_PATHS = ["/counts/sops", "/feed/sops", "/milk/sops", "/procurement/sops", "/weighing/sops"];
import {
  createSop,
  createSopVersion,
  dryRunSopVersion,
  getSop,
  getSopVersion,
  publishSopVersion,
  type CreateSOPVersionRequest,
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
  /** The version number that was published, so the editor can hand it to the library banner. */
  versionNumber?: number;
}

// publishSop publishes the immutable version (real RowVersionRequest). No fake publish state — the
// backend gate decides; a rejection surfaces verbatim.
export async function publishSop(sopId: string, versionId: string, rowVersion: number): Promise<PublishSopResult> {
  if (!sopId || !versionId) return { ok: false, message: "save the draft first" };
  const res = await publishSopVersion(sopId, versionId, rowVersion);
  if (!res.ok) return { ok: false, message: res.error.message ?? "publish failed", code: res.error.code };
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  return { ok: true, message: "Published — immutable version; tasks pin to it.", versionNumber: res.data.version.version };
}

// ---------------------------------------------------------------------------
// SOP-DRIVEN HERD OPERATIONS (maintainer decision 2026-09-13): operator steps (follow_up)
// ---------------------------------------------------------------------------

export interface FollowUpSaveResult {
  ok: boolean;
  message: string;
  code?: string;
  versionId?: string;
  rowVersion?: number;
  /** The version number that was published, so the editor can hand it to the library banner. */
  versionNumber?: number;
  report?: SOPValidationReport;
}

// saveFollowUpVersion creates a NEW draft version = the currently PUBLISHED version's capture
// form (fields / rules / proof_policy / compatibility, passed through verbatim -- the capture
// form is not edited here in P1) + the operator steps the editor emitted. The backend validates
// the follow_up against the tenant's Task Type Registry and refuses with the field named.
export async function saveFollowUpVersion(sopId: string, followUp: Record<string, unknown>, label?: string): Promise<FollowUpSaveResult> {
  if (!sopId) return { ok: false, message: "SOP id is required" };
  const detail = await getSop(sopId);
  if (!detail.ok) return { ok: false, message: detail.error.message ?? "load SOP failed", code: detail.error.code };
  const publishedId = detail.data.sop.active_sop_version_id ?? null;
  let base = detail.data.latest_version ?? null;
  if (publishedId && base?.sop_version_id !== publishedId) {
    const published = await getSopVersion(sopId, publishedId);
    if (published.ok) base = published.data.version;
  }
  if (!base) return { ok: false, message: "this SOP has no version to build on yet" };
  const baseDsl = (base.form_dsl ?? {}) as Record<string, unknown>;
  const version = await createSopVersion(sopId, {
    version_label: (label ?? "").trim() || `${detail.data.sop.name} · operator steps`,
    form_dsl: { ...baseDsl, follow_up: followUp },
    proof_policy: base.proof_policy as CreateSOPVersionRequest["proof_policy"],
    compatibility: (base.compatibility ?? undefined) as CreateSOPVersionRequest["compatibility"],
  });
  if (!version.ok) {
    return { ok: false, message: version.error.message ?? "create SOP version failed", code: version.error.code };
  }
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  const report = version.data.version.validation_report;
  return {
    ok: true,
    message: report?.valid ? "Operator steps saved as a draft version." : "Saved — backend flagged validation issues (see report).",
    versionId: version.data.version.sop_version_id,
    rowVersion: version.data.version.row_version,
    report,
  };
}

// publishFollowUpVersion saves, then publishes in one go: the next workflow opened on the phone
// runs these steps; open workflows keep theirs.
export async function publishFollowUpVersion(sopId: string, followUp: Record<string, unknown>, label?: string): Promise<FollowUpSaveResult> {
  const saved = await saveFollowUpVersion(sopId, followUp, label);
  if (!saved.ok || !saved.versionId || saved.rowVersion === undefined) return saved;
  const published = await publishSopVersion(sopId, saved.versionId, saved.rowVersion);
  if (!published.ok) {
    return { ok: false, message: published.error.message ?? "publish failed", code: published.error.code, versionId: saved.versionId };
  }
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  return { ok: true, message: `Published v${published.data.version.version}. New workflows use these steps from now on.`, versionId: saved.versionId, rowVersion: published.data.version.row_version, versionNumber: published.data.version.version };
}

// PROCUREMENT SOP (maintainer decision 2026-09-14): the inspection editor saves a new version = the
// published version's form_dsl (load form, rules, proof policy: unchanged) + the emitted
// `inspection` document. The backend validates the document (a version the phone could not run is
// refused with the field named); publishing makes it the catalog for animals recorded from then on.
export interface InspectionSaveResult {
  ok: boolean;
  message: string;
  code?: string;
  versionId?: string;
  rowVersion?: number;
  versionNumber?: number;
  report?: SOPValidationReport;
}

export async function saveInspectionVersion(sopId: string, inspection: Record<string, unknown>, label?: string): Promise<InspectionSaveResult> {
  if (!sopId) return { ok: false, message: "SOP id is required" };
  const detail = await getSop(sopId);
  if (!detail.ok) return { ok: false, message: detail.error.message ?? "SOP could not be read", code: detail.error.code };
  // Build on the version in force, never on a stray draft / retired version above it.
  const base = detail.data.published_version ?? detail.data.latest_version;
  if (!base) return { ok: false, message: "This SOP has no version to build on." };
  const formDsl = { ...(base.form_dsl as Record<string, unknown>), inspection };
  const version = await createSopVersion(sopId, {
    version_label: (label ?? "").trim() || `${detail.data.sop.name} · inspection`,
    form_dsl: formDsl,
    proof_policy: base.proof_policy as CreateSOPVersionRequest["proof_policy"],
  });
  if (!version.ok) return { ok: false, message: version.error.message ?? "create SOP version failed", code: version.error.code };
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  const report = version.data.version.validation_report;
  return {
    ok: true,
    message: report?.valid ? "Inspection saved as a draft version." : "Saved — backend flagged validation issues (see report).",
    versionId: version.data.version.sop_version_id,
    rowVersion: version.data.version.row_version,
    versionNumber: version.data.version.version,
    report,
  };
}

export async function publishInspectionVersion(sopId: string, inspection: Record<string, unknown>, label?: string): Promise<InspectionSaveResult> {
  const saved = await saveInspectionVersion(sopId, inspection, label);
  if (!saved.ok || !saved.versionId || saved.rowVersion === undefined) return saved;
  if (saved.report && !saved.report.valid) return { ...saved, ok: false, message: saved.report.errors?.[0]?.message ?? "The inspection has validation issues; fix them and publish again." };
  const res = await publishSopVersion(sopId, saved.versionId, saved.rowVersion);
  if (!res.ok) return { ok: false, message: res.error.message ?? "publish failed", code: res.error.code };
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  return { ...saved, ok: true, message: `Published v${saved.versionNumber ?? ""}. Animals recorded from now on use this inspection.` };
}

// WEIGHING SOP (maintainer decision 2026-09-15): the weighing rules editor saves a new version = the
// published version's form_dsl (capture form, rules, proof policy: unchanged) + the emitted
// `weighing` document. The backend validates the document (a version the planner could not run
// is refused with the field named); publishing makes it the rule set for tasks planned from
// then on -- a task already planned keeps the version it was planned on.
export type WeighingSaveResult = InspectionSaveResult;

export async function saveWeighingVersion(sopId: string, weighing: Record<string, unknown>, label?: string): Promise<WeighingSaveResult> {
  if (!sopId) return { ok: false, message: "SOP id is required" };
  const detail = await getSop(sopId);
  if (!detail.ok) return { ok: false, message: detail.error.message ?? "SOP could not be read", code: detail.error.code };
  const base = detail.data.published_version ?? detail.data.latest_version;
  if (!base) return { ok: false, message: "This SOP has no version to build on." };
  const formDsl = { ...(base.form_dsl as Record<string, unknown>), weighing };
  const version = await createSopVersion(sopId, {
    version_label: (label ?? "").trim() || `${detail.data.sop.name} · rules`,
    form_dsl: formDsl,
    proof_policy: base.proof_policy as CreateSOPVersionRequest["proof_policy"],
  });
  if (!version.ok) return { ok: false, message: version.error.message ?? "create SOP version failed", code: version.error.code };
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  const report = version.data.version.validation_report;
  return {
    ok: true,
    message: report?.valid ? "Weighing rules saved as a draft version." : "Saved — backend flagged validation issues (see report).",
    versionId: version.data.version.sop_version_id,
    rowVersion: version.data.version.row_version,
    versionNumber: version.data.version.version,
    report,
  };
}

export async function publishWeighingVersion(sopId: string, weighing: Record<string, unknown>, label?: string): Promise<WeighingSaveResult> {
  const saved = await saveWeighingVersion(sopId, weighing, label);
  if (!saved.ok || !saved.versionId || saved.rowVersion === undefined) return saved;
  if (saved.report && !saved.report.valid) return { ...saved, ok: false, message: saved.report.errors?.[0]?.message ?? "The weighing rules have validation issues; fix them and publish again." };
  const res = await publishSopVersion(sopId, saved.versionId, saved.rowVersion);
  if (!res.ok) return { ok: false, message: res.error.message ?? "publish failed", code: res.error.code };
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  return { ...saved, ok: true, message: `Published v${saved.versionNumber ?? ""}. Weighing tasks planned from now on run on these rules.` };
}

// FEED SOP (maintainer decision 2026-09-16): the feed cards editor saves a new version = the
// published version's form_dsl (capture form, rules, proof policy: unchanged) + the emitted `feed`
// document. The backend validates the document (a card the phone could not render is refused with
// the field named); publishing makes it the card for sheets issued from then on -- a sheet already
// issued keeps the card it was issued with.
export type FeedSaveResult = InspectionSaveResult;

export async function saveFeedVersion(sopId: string, feed: Record<string, unknown>, label?: string): Promise<FeedSaveResult> {
  if (!sopId) return { ok: false, message: "SOP id is required" };
  const detail = await getSop(sopId);
  if (!detail.ok) return { ok: false, message: detail.error.message ?? "SOP could not be read", code: detail.error.code };
  const base = detail.data.published_version ?? detail.data.latest_version;
  if (!base) return { ok: false, message: "This SOP has no version to build on." };
  const formDsl = { ...(base.form_dsl as Record<string, unknown>), feed };
  const version = await createSopVersion(sopId, {
    version_label: (label ?? "").trim() || `${detail.data.sop.name} · card`,
    form_dsl: formDsl,
    proof_policy: base.proof_policy as CreateSOPVersionRequest["proof_policy"],
  });
  if (!version.ok) return { ok: false, message: version.error.message ?? "create SOP version failed", code: version.error.code };
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  const report = version.data.version.validation_report;
  return {
    ok: true,
    message: report?.valid ? "Feed card saved as a draft version." : "Saved — backend flagged validation issues (see report).",
    versionId: version.data.version.sop_version_id,
    rowVersion: version.data.version.row_version,
    versionNumber: version.data.version.version,
    report,
  };
}

export async function publishFeedVersion(sopId: string, feed: Record<string, unknown>, label?: string): Promise<FeedSaveResult> {
  const saved = await saveFeedVersion(sopId, feed, label);
  if (!saved.ok || !saved.versionId || saved.rowVersion === undefined) return saved;
  if (saved.report && !saved.report.valid) return { ...saved, ok: false, message: saved.report.errors?.[0]?.message ?? "The feed card has validation issues; fix it and publish again." };
  const res = await publishSopVersion(sopId, saved.versionId, saved.rowVersion);
  if (!res.ok) return { ok: false, message: res.error.message ?? "publish failed", code: res.error.code };
  for (const path of SOP_PAGE_PATHS) revalidatePath(path);
  return { ...saved, ok: true, message: `Published v${saved.versionNumber ?? ""}. Sheets issued from now on run on this card.` };
}
