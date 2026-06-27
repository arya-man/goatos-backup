// Pure rule_dsl builder + gates. Shared by the client modal (live JSONB preview) and
// the server action (persisted rule_dsl), so the two cannot drift. The editor is category/schema
// driven: vaccination renders dose/lot policy; feed_direction renders ration/session/inventory policy.

export interface DoseRow {
  doseCode: string;
  trigger: string;
  offsetDays: number;
  dueWindowDays: number;
  repeat: string;
  repeatUntilAfterAge: string;
  minGapDays: number;
  catchUp: string;
  sopVersion: string;
  proofCsv: string;
}

export interface Eligibility {
  stage: string;
  sex: string;
  breed: string;
  lifecycle: string;
  health: string;
  reproductive: string;
  deferStates: string[];
  individualOverride: boolean;
}

export interface FeedFields {
  animalStage: string;
  breedClass: string;
  feedItem: string;
  quantity: number;
  unit: string;
  sessionTimes: string;
  packingProofCsv: string;
  executionProofCsv: string;
  inventoryPolicy: string;
}

export interface SourceMeta {
  sourceSystem: string;
  sourceRef: string;
  reviewStatus: string;
  reviewedBy: string;
  approvedBy: string;
  approvedAt: string;
}

export interface RuleInput {
  category: string;
  code: string;
  name: string;
  scope: string;
  effectiveFrom: string;
  // sopVersionId is a REAL published SOP version UUID (selected from the SOP Library), bound at the
  // protocol-version level. Backend publish (publish.go ValidateExecutionContract) requires it; an empty
  // value keeps the version a draft that cannot publish.
  sopVersionId: string;
  eligibility: Eligibility;
  vaccineLotPolicy: string;
  missedDosePolicy: string;
  escalation: string;
  source: SourceMeta;
  doses: DoseRow[];
  feed: FeedFields;
}

// A published SOP version the author can bind to a protocol version (real UUID + display label).
export interface SopVersionOption {
  id: string;
  label: string;
}

// A backend animal-stage option (one active animal_stage_lookup row) for the Config stage picker.
// `code` is the stable stage_code authored into rule_dsl.eligibility.animal_stage; `label` is for
// display. These come from the backend — the frontend never hardcodes the stage vocabulary.
export interface AnimalStageOption {
  code: string;
  label: string;
}

export interface ProtocolRuleDraft {
  doseCode: string;
  trigger: string;
  offsetDays: number;
  dueWindowDays: number;
  repeat: string;
  repeatUntilAfterAge: string;
  minGapDays: number;
  catchUp: string;
  sopVersion: string;
  proofPolicy: string[];
  sortOrder: number;
}

// Stage bands (K0/K1/K2…) are NOT hardcoded here. They are backend reference data from
// animal_stage_lookup, loaded via listAnimalStages and passed in as AnimalStageOption[] (PHC
// vaccination TRD: stage bands live in the lookup). The only stage literal the UI owns is the
// ALL_STAGES filter below, which is a UI scope ("every stage"), not an animal_stage_lookup row.
export const ALL_STAGES_VALUE = "all";

export const NEXT_DUE_BASIS = "last_accepted_completion_else_dob";
export const STAGE_SOURCE = "shed_profiles.animal_stage_id -> animal_stage_lookup";

export function isRfc3339Timestamp(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return false;
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:\d{2})$/.exec(trimmed);
  if (!match) return false;

  const [, yearRaw, monthRaw, dayRaw, hourRaw, minuteRaw, secondRaw, offsetRaw] = match;
  const year = Number(yearRaw);
  const month = Number(monthRaw);
  const day = Number(dayRaw);
  const hour = Number(hourRaw);
  const minute = Number(minuteRaw);
  const second = Number(secondRaw);
  if (hour > 23 || minute > 59 || second > 59) return false;
  if (offsetRaw !== "Z") {
    const offsetHour = Number(offsetRaw.slice(1, 3));
    const offsetMinute = Number(offsetRaw.slice(4, 6));
    if (offsetHour > 23 || offsetMinute > 59) return false;
  }

  const candidate = new Date(Date.UTC(year, month - 1, day));
  return candidate.getUTCFullYear() === year && candidate.getUTCMonth() === month - 1 && candidate.getUTCDate() === day;
}

export function newDose(seq: number): DoseRow {
  return {
    doseCode: seq === 1 ? "primary" : `dose_${seq}`,
    trigger: seq === 1 ? "birth_age" : "after_previous_completion",
    offsetDays: seq === 1 ? 21 : 30,
    dueWindowDays: 7,
    repeat: "none",
    repeatUntilAfterAge: "-",
    minGapDays: seq === 1 ? 0 : 14,
    catchUp: "phc_approval",
    sopVersion: "",
    proofCsv: "shed,vial,dose,lot,qty",
  };
}

export function newFeedFields(): FeedFields {
  return {
    animalStage: "all",
    breedClass: "all",
    feedItem: "custom",
    quantity: 1,
    unit: "kg",
    sessionTimes: "09:00,15:00",
    packingProofCsv: "pack_qty,feed_item,lot,video",
    executionProofCsv: "distribution_video,consumed_qty,water_check",
    inventoryPolicy: "reserve_consume_release",
  };
}

export function parseScope(scope: string): { type: string; id: string | null } {
  const [type, id] = scope.split(":");
  return { type, id: id ?? null };
}

function csvToArr(csv: string): string[] {
  return csv
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
}

function sourceDsl(source: SourceMeta): Record<string, unknown> {
  const sourceSystem = source.sourceSystem.trim();
  const reviewStatus = source.reviewStatus.trim();
  const sourceRef = source.sourceRef.trim();
  const reviewedBy = source.reviewedBy.trim();
  const approvedBy = source.approvedBy.trim();
  const approvedAt = source.approvedAt.trim();
  return {
    source_system: sourceSystem,
    source_ref: sourceRef,
    imported_at: sourceSystem !== "manual_admin" ? "(on import)" : null,
    reviewed_by: reviewedBy,
    review_status: reviewStatus,
    approved_by: approvedBy,
    approved_at: reviewStatus === "approved" && approvedAt ? approvedAt : null,
  };
}

function vaccinationDsl(input: RuleInput): Record<string, unknown> {
  return {
    category: input.category,
    scope: parseScope(input.scope),
    eligibility: {
      animal_stage: input.eligibility.stage,
      animal_stage_source: STAGE_SOURCE,
      sex: input.eligibility.sex,
      breed: input.eligibility.breed,
      lifecycle: input.eligibility.lifecycle,
      health: input.eligibility.health,
      reproductive: input.eligibility.reproductive,
      defer_states: input.eligibility.deferStates,
      individual_override: input.eligibility.individualOverride,
    },
    missed_dose_policy: input.missedDosePolicy,
    next_due_basis: NEXT_DUE_BASIS,
    stock_policy: {
      vaccine_lot_requirement: input.vaccineLotPolicy,
      pick: "FEFO",
      reject_expired_lot: true,
      cold_chain_required: true,
    },
    schedule: input.doses.map((d, i) => ({
      dose_code: d.doseCode,
      sequence: i + 1,
      trigger_type: d.trigger,
      offset_days: Number(d.offsetDays) || 0,
      due_window_days: Number(d.dueWindowDays) || 0,
      min_gap_days: Number(d.minGapDays) || 0,
      repeat: d.repeat,
      repeat_until_after_age: d.repeatUntilAfterAge,
      catch_up: d.catchUp,
      // sop_label is a DISPLAY label only. The executable SOP binds at version level
      // (sop_version_id). It is deliberately NOT emitted as schedule[].sop_version, because the
      // backend execution contract (publish.go) treats a non-empty row sop_version as a valid
      // executable fallback — a display-only label must never satisfy that gate.
      sop_label: d.sopVersion,
      proof_policy: csvToArr(d.proofCsv),
    })),
    escalation: input.escalation,
    source: sourceDsl(input.source),
  };
}

function feedSessions(feed: FeedFields): string[] {
  const sessions = csvToArr(feed.sessionTimes);
  return sessions.length > 0 ? sessions : ["09:00"];
}

function feedDsl(input: RuleInput): Record<string, unknown> {
  const packingProof = csvToArr(input.feed.packingProofCsv);
  const executionProof = csvToArr(input.feed.executionProofCsv);
  return {
    category: "feed_direction",
    scope: parseScope(input.scope),
    eligibility: {
      animal_stage: input.feed.animalStage,
      animal_stage_source: STAGE_SOURCE,
      breed_class: input.feed.breedClass,
    },
    ration: {
      feed_item: input.feed.feedItem,
      quantity: Number(input.feed.quantity) || 0,
      unit: input.feed.unit,
    },
    session_timing: feedSessions(input.feed).map((session_time, i) => ({
      session_order: i + 1,
      session_time,
      packing_proof_policy: packingProof,
      execution_proof_policy: executionProof,
    })),
    inventory_policy: {
      mode: input.feed.inventoryPolicy,
      reserve: "reserve stock before packing",
      consume: "consume verified quantity",
      release: "release unused reserved quantity",
    },
    source: sourceDsl(input.source),
  };
}

// buildRuleDsl is the canonical authored ruleset stored at protocol_versions.rule_dsl.
export function buildRuleDsl(input: RuleInput): Record<string, unknown> {
  if (input.category === "feed_direction") return feedDsl(input);
  return vaccinationDsl(input);
}

// proofTokens collects the authored proof tokens for the active category (per-dose proof for
// vaccination, packing+execution for feed). Single source for buildProofPolicy + hasProofRequirement.
function proofTokens(input: RuleInput): string[] {
  return input.category === "feed_direction"
    ? [...csvToArr(input.feed.packingProofCsv), ...csvToArr(input.feed.executionProofCsv)]
    : input.doses.flatMap((d) => csvToArr(d.proofCsv));
}

// buildProofPolicy derives the version-level proof_policy object from the authored proof tokens.
// Backend publish requires a proof_policy carrying a REAL requirement (rawProofHasContent), so an
// empty token set yields { required_proofs: [] } which the backend now rejects — gate publish with
// hasProofRequirement so the UI fails fast with a clear reason instead of a backend 422.
export function buildProofPolicy(input: RuleInput): Record<string, unknown> {
  return { required_proofs: Array.from(new Set(proofTokens(input))) };
}

// hasProofRequirement is true when the author supplied at least one real proof token. Mirrors the
// backend rawProofHasContent gate so Publish can be blocked client-side with an actionable message.
export function hasProofRequirement(input: RuleInput): boolean {
  return proofTokens(input).length > 0;
}

export function buildProtocolRuleRows(input: RuleInput): ProtocolRuleDraft[] {
  if (input.category === "feed_direction") {
    const proofPolicy = [...csvToArr(input.feed.packingProofCsv), ...csvToArr(input.feed.executionProofCsv)];
    return feedSessions(input.feed).map((session, i) => ({
      doseCode: `feed_session_${i + 1}_${session.replace(/[^0-9A-Za-z]/g, "") || "slot"}`,
      trigger: "calendar",
      offsetDays: 0,
      dueWindowDays: 1,
      minGapDays: 0,
      repeat: "every_n_days",
      repeatUntilAfterAge: "-",
      catchUp: "next_cycle",
      sopVersion: "",
      proofPolicy,
      sortOrder: i + 1,
    }));
  }

  return input.doses.map((d, i) => ({
    doseCode: d.doseCode,
    trigger: d.trigger,
    offsetDays: Number(d.offsetDays) || 0,
    dueWindowDays: Number(d.dueWindowDays) || 0,
    minGapDays: Number(d.minGapDays) || 0,
    repeat: d.repeat,
    repeatUntilAfterAge: d.repeatUntilAfterAge,
    catchUp: d.catchUp,
    sopVersion: d.sopVersion,
    proofPolicy: csvToArr(d.proofCsv),
    sortOrder: i + 1,
  }));
}

export interface SourceBadge {
  text: string;
  tone: "warn" | "info" | "ok";
}

export type SourceSystemContractOption = { key: string; label: string; tone: string };

export type SourceBadgeCopy = {
  notSourceBacked: string;
  notPublishable: string;
  approved: string;
  pending: string;
  sourceRefNeeded: string;
};

export type PublishGateCopy = {
  sourceSystem: string;
  sourceRef: string;
  reviewStatus: string;
  approvedBy: string;
  approvedAt: string;
  approvedAtRFC3339: string;
};

function sourceOption(sourceSystem: string, options: SourceSystemContractOption[]): SourceSystemContractOption | undefined {
  return options.find((option) => option.key === sourceSystem.trim());
}

export function sourceBadge(source: SourceMeta, options: SourceSystemContractOption[], labels: SourceBadgeCopy): SourceBadge {
  const sourceSystem = source.sourceSystem.trim();
  const reviewStatus = source.reviewStatus.trim();
  const option = sourceOption(sourceSystem, options);
  const sourceLabel = option?.label ?? sourceSystem;
  if (!option || option.tone === "warn") return { text: labels.notSourceBacked, tone: "warn" };
  if (option.tone !== "ok") {
    return { text: `${labels.notPublishable} - ${sourceLabel}`, tone: "warn" };
  }
  if (
    reviewStatus === "approved" &&
    source.sourceRef.trim() &&
    source.approvedBy.trim() &&
    isRfc3339Timestamp(source.approvedAt)
  ) {
    return { text: `${labels.approved} - ${sourceLabel}`, tone: "ok" };
  }
  return { text: `${labels.pending}${source.sourceRef.trim() ? "" : ` - ${labels.sourceRefNeeded}`}`, tone: "info" };
}

// validatePublish mirrors the backend source-backed gate using backend-owned source_systems metadata.
// The backend remains authoritative on submit; this only drives the pre-submit disabled reason.
export function validatePublish(source: SourceMeta, publishableSourceKeys: Set<string>, labels: PublishGateCopy): { ok: boolean; message?: string } {
  if (!publishableSourceKeys.has(source.sourceSystem.trim())) {
    return { ok: false, message: labels.sourceSystem };
  }
  if (!source.sourceRef.trim()) return { ok: false, message: labels.sourceRef };
  if (source.reviewStatus.trim() !== "approved") return { ok: false, message: labels.reviewStatus };
  if (!source.approvedBy.trim()) return { ok: false, message: labels.approvedBy };
  if (!source.approvedAt.trim()) return { ok: false, message: labels.approvedAt };
  if (!isRfc3339Timestamp(source.approvedAt)) return { ok: false, message: labels.approvedAtRFC3339 };
  return { ok: true };
}
