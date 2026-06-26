// Pure rule_dsl builder + option vocab + gates. Shared by the client modal (live JSONB preview) and
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

// ---- option vocab (mirrors mock New-draft-rule modal + obligation-engine config contract) ----
// Only categories with a real DSL builder + backend support are exposed. vaccination is the live slice;
// feed_direction is authorable (category/schema-driven editor) but not publishable yet (see backend
// publishableSources). deworming/biosecurity/etc. are unbuilt and must not be shown as live
// (context/frontend/current-admin-web-scope.md).
export const CATEGORIES = ["vaccination", "feed_direction"];
export const SCOPES = ["tenant", "park:CBE", "park:CPT"];
// Stage bands (K0/K1/K2…) are NOT hardcoded here. They are backend reference data from
// animal_stage_lookup, loaded via listAnimalStages and passed in as AnimalStageOption[] (PHC
// vaccination TRD: stage bands live in the lookup). The only stage literal the UI owns is the
// ALL_STAGES filter below, which is a UI scope ("every stage"), not an animal_stage_lookup row.
export const ALL_STAGES_VALUE = "all";
export const SEXES = ["all", "female", "male"];
export const BREEDS = ["all", "Beetal", "Sirohi", "Boer×"];
export const HEALTHS = ["healthy", "any"];
export const LIFECYCLES = ["active", "any"];
export const REPRODUCTIVE = ["any", "exclude_pregnant", "exclude_lactating", "pregnant_only"];
export const DEFER_STATES = ["ICU", "quarantine", "sick"];
export const MISSED_DOSE_POLICIES = [
  { value: "immediate", label: "catch up immediately" },
  { value: "next_cycle", label: "skip to next cycle" },
  { value: "phc_approval", label: "require PHC approval" },
  { value: "defer", label: "defer with reason" },
];
export const SOURCE_SYSTEMS = [
  { value: "manual_admin", label: "manual admin (not publishable)" },
  { value: "vaccinations_db", label: "Vaccinations DB" },
  { value: "phc", label: "PHC" },
  { value: "vet", label: "vet" },
  { value: "feed_master", label: "Feed master" },
  { value: "nutritionist", label: "nutritionist" },
  { value: "ops_source", label: "operations source" },
];
export const REVIEW_STATUSES = ["extracted", "reviewed", "approved"];
export const TRIGGER_TYPES = ["birth_age", "post_arrival", "calendar", "after_previous_completion", "manual_campaign"];
export const REPEATS = ["none", "every_n_days", "yearly", "until_age", "after_age"];
export const CATCH_UPS = ["immediate", "next_cycle", "phc_approval", "defer"];
export const SOP_VERSIONS = ["vacc-sop v2", "deworm-sop v1"];
export const FEED_CLASSES = ["all", "kid", "grower", "fattening", "pregnant_lactating", "buck", "custom"];
export const FEED_ITEMS = ["Mesha concentrate", "Masoor bhusa", "Toor dhal bhusa", "Green feed", "Dry feed", "custom"];
export const FEED_UNITS = ["kg", "g", "litre", "bundle"];
export const FEED_INVENTORY_POLICIES = [
  { value: "reserve_consume_release", label: "reserve before pack -> consume verified qty -> release remainder" },
  { value: "consume_on_verify", label: "consume only after verifier accepts execution proof" },
  { value: "manual_hold", label: "manual reserve hold; release by Data Ops" },
];

export const NEXT_DUE_BASIS = "last_accepted_completion_else_dob";
export const STAGE_SOURCE = "shed_profiles.animal_stage_id -> animal_stage_lookup";

// SOURCE_BACKED: sources that count as authored-from-a-real-source (not manual_admin). PUBLISHABLE_SOURCES
// is the stricter set the BACKEND will actually publish (backend/internal/protocol/app/publish.go
// publishableSources) — vaccination protocol sources only. Feed/nutrition sources are source-backed for
// authoring but cannot publish yet; the client gate must mirror the backend so it never claims a draft is
// publishable when publish would 422.
const SOURCE_BACKED = ["vaccinations_db", "phc", "vet", "feed_master", "nutritionist", "ops_source"];
const PUBLISHABLE_SOURCES = ["vaccinations_db", "phc", "vet"];

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
    sopVersion: "vacc-sop v2",
    proofCsv: "shed,vial,dose,lot,qty",
  };
}

export function newFeedFields(): FeedFields {
  return {
    animalStage: "all",
    breedClass: "all",
    feedItem: "Mesha concentrate",
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
  return {
    source_system: source.sourceSystem,
    source_ref: source.sourceRef,
    imported_at: source.sourceSystem !== "manual_admin" ? "(on import)" : null,
    reviewed_by: source.reviewedBy,
    review_status: source.reviewStatus,
    approved_by: source.approvedBy,
    approved_at: source.reviewStatus === "approved" ? "(on approve)" : null,
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
      // executable fallback — a fake label like "vacc-sop v2" must never satisfy that gate.
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
      sopVersion: "feed-sop v1",
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

export function sourceBadge(source: SourceMeta): SourceBadge {
  const labels: Record<string, string> = {
    vaccinations_db: "Vaccinations DB",
    phc: "PHC",
    vet: "vet",
    feed_master: "Feed master",
    nutritionist: "nutritionist",
    ops_source: "operations source",
    manual_admin: "manual admin",
  };
  const sourced = SOURCE_BACKED.includes(source.sourceSystem);
  const publishable = PUBLISHABLE_SOURCES.includes(source.sourceSystem);
  if (!sourced) return { text: "Draft - not source-backed - cannot publish", tone: "warn" };
  if (!publishable) {
    return { text: `Draft - ${labels[source.sourceSystem]} - not a publishable source (vaccinations_db/phc/vet only)`, tone: "warn" };
  }
  if (source.reviewStatus === "approved" && source.sourceRef && source.approvedBy) {
    return { text: `Approved - publishable - ${labels[source.sourceSystem]}`, tone: "ok" };
  }
  return { text: `Draft - source-backed - pending approval${source.sourceRef ? "" : " - source_ref required"}`, tone: "info" };
}

// validatePublish mirrors the backend source-backed gate (publish.go ValidatePublishable). The source
// system must be one the backend will publish (vaccinations_db/phc/vet); feed/nutrition sources are
// authorable but not publishable, so the client rejects them up front instead of letting publish 422.
export function validatePublish(source: SourceMeta): { ok: boolean; message?: string } {
  if (!PUBLISHABLE_SOURCES.includes(source.sourceSystem)) {
    return { ok: false, message: "Publish blocked - source_system must be vaccinations_db, phc, or vet" };
  }
  if (!source.sourceRef) return { ok: false, message: "Publish blocked - source_ref required" };
  if (source.reviewStatus !== "approved") return { ok: false, message: "Publish blocked - review_status must be approved" };
  if (!source.approvedBy) return { ok: false, message: "Publish blocked - approved_by required" };
  return { ok: true };
}
