// Pure rule_dsl builder + gates. Shared by the client modal (live JSONB preview) and
// the server action (persisted rule_dsl), so the two cannot drift. The editor is category/schema
// driven: vaccination renders dose/lot policy; feed_direction renders ration/session/inventory policy.

export interface DoseRow {
  doseCode: string;
  trigger: string;
  offsetDays: number;
  dueWindowDays: number;
  doseAmount: number;
  doseUnit: string;
  vialDoses: number;
  revaccinationIntervalDays: number;
  scheduleNote: string;
  routeSite: string;
  maxDelayDays: number;
  courseLapsePolicy: string;
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
  excludeReproductiveStates: string[];
  deferStates: string[];
}

export interface VaccineMatrix {
  code: string;
  name: string;
  type: string;
  pathogenClass: string;
  courseType: string;
  inventoryItemId: string;
  manufacturer: string;
  disease: string;
  compatibilityGroup: string;
}

export interface VaccinationMatrixRow {
  id: string;
  vaccine: VaccineMatrix;
  stage: string;
  sex: string;
  breed: string;
  doses?: DoseRow[];
}

export interface FeedFields {
  animalStage: string;
  breedClass: string;
  sourceTables: string[];
  parameterFamilies: string[];
  dimensionKeys: string[];
  ratioPolicy: string;
  feedItem: string;
  quantity: number;
  unit: string;
  sessionTimes: string;
  slotWeights: string;
  packingProofCsv: string;
  executionProofCsv: string;
  inventoryPolicy: string;
  validationChecks: string[];
  calculationOutputs: string[];
}

export interface CompatibilityPolicy {
  liveToKilledGapDays: number;
  killedToKilledGapDays: number;
  liveToLiveGapDays: number;
  kidBoosterMinGapDays: number;
  bacterialViralSameDayAllowed: boolean;
  liveKilledViralSameDayAllowed: boolean;
}

export interface ProcurementPolicy {
  warmupNoVaccinationDays: number;
  kidsNormalScheduleUntilWeeks: number;
  adultPriorVaccinationAllowed: boolean;
  firstWave: string;
  secondWaveAfterDays: number;
  goatSecondWave: string;
}

export interface PregnancyPolicy {
  allowUntilPregnancyMonth: number;
  skipFromPregnancyMonth: number;
  skipThroughPregnancyMonth: number;
  postDeliveryCatchUpDays: number;
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
  vaccine: VaccineMatrix;
  eligibility: Eligibility;
  vaccineLotPolicy: string;
  missedDosePolicy: string;
  escalation: string;
  compatibilityPolicy: CompatibilityPolicy;
  procurementPolicy: ProcurementPolicy;
  pregnancyPolicy: PregnancyPolicy;
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
// animal_stage_lookup, loaded via listAnimalStages and passed in as AnimalStageOption[]
// (Preventive Care (PC) vaccination TRD: stage bands live in the lookup). The only stage literal the UI owns is the
// ALL_STAGES filter below, which is a UI scope ("every stage"), not an animal_stage_lookup row.
export const ALL_STAGES_VALUE = "all";

export const STAGE_SOURCE = "shed_profiles.animal_stage_id -> animal_stage_lookup";

export function newDose(seq: number): DoseRow {
  return {
    doseCode: seq === 1 ? "primary" : `dose_${seq}`,
    trigger: seq === 1 ? "birth_age" : "after_previous_completion",
    offsetDays: seq === 1 ? 28 : 49,
    dueWindowDays: 7,
    doseAmount: 2,
    doseUnit: "ml",
    vialDoses: 0,
    revaccinationIntervalDays: 0,
    scheduleNote: "",
    routeSite: "subcutaneous",
    maxDelayDays: 7,
    courseLapsePolicy: "phc_review",
    repeat: "none",
    repeatUntilAfterAge: "-",
    minGapDays: seq === 1 ? 0 : 21,
    catchUp: "phc_approval",
    sopVersion: "",
    proofCsv: "shed,vial,dose,lot,qty",
  };
}

export function newFeedFields(): FeedFields {
  return {
    animalStage: "all",
    breedClass: "all_reviewed_cohorts",
    sourceTables: [],
    parameterFamilies: [],
    dimensionKeys: [],
    ratioPolicy: "source_row_variable",
    feedItem: "reviewed_template_rows",
    quantity: 0,
    unit: "kg_as_fed",
    sessionTimes: "09:00,15:00",
    slotWeights: "50,50",
    packingProofCsv: "pack_qty,feed_item,lot,video",
    executionProofCsv: "distribution_video,consumed_qty,wastage_qty,water_check",
    inventoryPolicy: "reserve_consume_release",
    validationChecks: [],
    calculationOutputs: [],
  };
}

export function newCompatibilityPolicy(): CompatibilityPolicy {
  return {
    liveToKilledGapDays: 14,
    killedToKilledGapDays: 14,
    liveToLiveGapDays: 28,
    kidBoosterMinGapDays: 21,
    bacterialViralSameDayAllowed: true,
    liveKilledViralSameDayAllowed: true,
  };
}

export function newProcurementPolicy(): ProcurementPolicy {
  return {
    warmupNoVaccinationDays: 7,
    kidsNormalScheduleUntilWeeks: 16,
    adultPriorVaccinationAllowed: true,
    firstWave: "ET+TT,PPR",
    secondWaveAfterDays: 28,
    goatSecondWave: "Goat Pox,ET+TT booster",
  };
}

export function newPregnancyPolicy(): PregnancyPolicy {
  return {
    allowUntilPregnancyMonth: 3,
    skipFromPregnancyMonth: 4,
    skipThroughPregnancyMonth: 5,
    postDeliveryCatchUpDays: 14,
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

function vaccinationDsl(input: RuleInput): Record<string, unknown> {
  return {
    category: input.category,
    scope: parseScope(input.scope),
    vaccine: {
      code: input.vaccine.code.trim() || input.code.trim(),
      name: input.vaccine.name.trim() || input.name.trim(),
      type: input.vaccine.type,
      pathogen_class: input.vaccine.pathogenClass,
      course_type: input.vaccine.courseType,
      inventory_item_id: input.vaccine.inventoryItemId.trim() || null,
      manufacturer: input.vaccine.manufacturer.trim() || null,
      disease: input.vaccine.disease.trim() || input.name.trim() || null,
      compatibility_group: input.vaccine.compatibilityGroup.trim() || input.vaccine.code.trim() || input.code.trim(),
    },
    eligibility: {
      animal_stage: input.eligibility.stage,
      animal_stage_source: STAGE_SOURCE,
      sex: input.eligibility.sex,
      breed: input.eligibility.breed,
      lifecycle: input.eligibility.lifecycle,
      health: input.eligibility.health,
      reproductive: input.eligibility.reproductive,
      exclude_reproductive_states: input.eligibility.excludeReproductiveStates,
      defer_states: input.eligibility.deferStates,
    },
    missed_dose_policy: input.missedDosePolicy,
    stock_policy: {
      vaccine_lot_requirement: input.vaccineLotPolicy,
      pick: "FEFO",
      reject_expired_lot: true,
      cold_chain_required: true,
    },
    compatibility_policy: {
      live_to_killed_gap_days: Number(input.compatibilityPolicy.liveToKilledGapDays) || 0,
      killed_to_killed_gap_days: Number(input.compatibilityPolicy.killedToKilledGapDays) || 0,
      live_to_live_gap_days: Number(input.compatibilityPolicy.liveToLiveGapDays) || 0,
      kid_booster_min_gap_days: Number(input.compatibilityPolicy.kidBoosterMinGapDays) || 0,
      bacterial_viral_same_day_allowed: input.compatibilityPolicy.bacterialViralSameDayAllowed,
      live_killed_viral_same_day_allowed: input.compatibilityPolicy.liveKilledViralSameDayAllowed,
    },
    procurement_policy: {
      warmup_no_vaccination_days: Number(input.procurementPolicy.warmupNoVaccinationDays) || 0,
      kids_normal_schedule_until_weeks: Number(input.procurementPolicy.kidsNormalScheduleUntilWeeks) || 0,
      adult_prior_vaccination_allowed: input.procurementPolicy.adultPriorVaccinationAllowed,
      first_wave: csvToArr(input.procurementPolicy.firstWave),
      second_wave_after_days: Number(input.procurementPolicy.secondWaveAfterDays) || 0,
      goat_second_wave: csvToArr(input.procurementPolicy.goatSecondWave),
    },
    pregnancy_policy: {
      allow_until_pregnancy_month: Number(input.pregnancyPolicy.allowUntilPregnancyMonth) || 0,
      skip_from_pregnancy_month: Number(input.pregnancyPolicy.skipFromPregnancyMonth) || 0,
      skip_through_pregnancy_month: Number(input.pregnancyPolicy.skipThroughPregnancyMonth) || 0,
      post_delivery_catch_up_days: Number(input.pregnancyPolicy.postDeliveryCatchUpDays) || 0,
    },
    schedule: input.doses.map((d, i) => ({
      dose_code: d.doseCode,
      sequence: i + 1,
      trigger_type: d.trigger,
      offset_days: Number(d.offsetDays) || 0,
      due_window_days: Number(d.dueWindowDays) || 0,
      dose_amount: Number(d.doseAmount) || 0,
      dose_unit: d.doseUnit,
      vial_doses: Number(d.vialDoses) || 0,
      revaccination_interval_days: Number(d.revaccinationIntervalDays) || 0,
      schedule_note: d.scheduleNote.trim() || null,
      route_site: d.routeSite,
      max_delay_days: Number(d.maxDelayDays) || Number(d.dueWindowDays) || 0,
      course_lapse_policy: d.courseLapsePolicy,
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
  };
}

function feedSessions(feed: FeedFields): string[] {
  const sessions = csvToArr(feed.sessionTimes);
  return sessions.length > 0 ? sessions : ["09:00"];
}

function feedSlotWeights(feed: FeedFields): string[] {
  return csvToArr(feed.slotWeights);
}

function feedDsl(input: RuleInput): Record<string, unknown> {
  const packingProof = csvToArr(input.feed.packingProofCsv);
  const executionProof = csvToArr(input.feed.executionProofCsv);
  const weights = feedSlotWeights(input.feed);
  return {
    category: "feed_direction",
    scope: parseScope(input.scope),
    eligibility: {
      animal_stage: input.feed.animalStage,
      animal_stage_source: STAGE_SOURCE,
      breed_class: input.feed.breedClass,
    },
    parameter_template: {
      source_tables: input.feed.sourceTables,
      parameter_families: input.feed.parameterFamilies,
      dimension_keys: input.feed.dimensionKeys,
      ratio_policy: input.feed.ratioPolicy,
    },
    ration: {
      mode: "reviewed_template_rows",
      feed_item: input.feed.feedItem,
      quantity: Number(input.feed.quantity) || 0,
      unit: input.feed.unit,
      quantity_semantics: "source-row variable by approved dimensions; examples are not global defaults",
    },
    session_timing: feedSessions(input.feed).map((session_time, i) => ({
      session_order: i + 1,
      session_time,
      split_weight: weights[i] ?? null,
      packing_proof_policy: packingProof,
      execution_proof_policy: executionProof,
    })),
    inventory_policy: {
      mode: input.feed.inventoryPolicy,
      reserve: "reserve stock before packing",
      consume: "consume verified quantity",
      release: "release unused reserved quantity",
    },
    validation_policy: {
      checks: input.feed.validationChecks,
      calculation_outputs: input.feed.calculationOutputs,
      fail_closed: true,
      preview_required: true,
    },
  };
}

// buildRuleDsl is the canonical authored ruleset stored at protocol_versions.rule_dsl.
export function buildRuleDsl(input: RuleInput): Record<string, unknown> {
  if (input.category === "feed_direction") return feedDsl(input);
  return vaccinationDsl(input);
}

export function ruleInputForVaccinationMatrixRow(input: RuleInput, row: VaccinationMatrixRow, index: number, total: number): RuleInput {
  if (input.category !== "vaccination") return input;
  const suffix = compactSlug([row.vaccine.code, row.stage, row.sex, row.breed]).join("_") || `row_${index + 1}`;
  return {
    ...input,
    code: total > 1 ? `${input.code}.${suffix}` : input.code,
    name: total > 1 ? `${input.name} - ${row.vaccine.code || row.vaccine.name || `row ${index + 1}`} ${row.stage}/${row.breed}` : input.name,
    vaccine: row.vaccine,
    eligibility: {
      ...input.eligibility,
      stage: row.stage,
      sex: row.sex,
      breed: row.breed,
    },
    doses: row.doses !== undefined ? row.doses : input.doses,
  };
}

export function buildVaccinationMatrixPreview(input: RuleInput, rows: VaccinationMatrixRow[]): Record<string, unknown> {
  if (input.category !== "vaccination") return buildRuleDsl(input);
  if (rows.length <= 1 && rows[0]) return buildRuleDsl(ruleInputForVaccinationMatrixRow(input, rows[0], 0, 1));
  return {
    category: input.category,
    scope: parseScope(input.scope),
    matrix_rows: rows.map((row, index) => buildRuleDsl(ruleInputForVaccinationMatrixRow(input, row, index, rows.length))),
  };
}

function compactSlug(values: string[]): string[] {
  return values
    .map((value) => slugPart(value))
    .filter(Boolean);
}

function slugPart(value: string): string {
  const slug = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .replace(/_{2,}/g, "_");
  if (!slug || slug === "all" || slug === "any") return "";
  return /^[a-z]/.test(slug) ? slug : `r_${slug}`;
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
