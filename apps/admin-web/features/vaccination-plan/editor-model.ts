/**
 * The editor's working copy of a plan, and the mapping back to rule_dsl.
 *
 * Two rules govern this file:
 *
 * 1. NOTHING IS INVENTED. Every value shown comes from the stored document. A
 *    field the farm never set stays absent; it is never filled with a plausible
 *    default that would then be saved as though someone had chosen it.
 *
 * 2. THE DOCUMENT IS PRESERVED. toRuleDsl edits a deep copy of what was read and
 *    writes back only the fields the editor owns. Everything else -- capacity,
 *    drive policy, pregnancy policy, compatibility gaps, dose amounts, vial
 *    sizes, route -- rides along untouched. A config screen that silently drops
 *    the keys it does not render would delete live scheduling behaviour.
 */

import type { ScheduleRule } from "./plan-model";
import type { ProtocolVersionRule } from "@/lib/api/server";

export type Dose = {
  /** Days from the trigger. First dose: age. Booster: gap after the previous dose. */
  offsetDays: number;
  triggerType: string;
  doseCode: string;
};

export type EditorVaccine = {
  code: string;
  name: string;
  vaccineClass: string;
  disease: string;
  /** In the plan at all. Off means the row keeps its identity but has no schedule. */
  on: boolean;
  /** Doses given from the animal's date of birth, in order. */
  kidDoses: Dose[];
  /** Doses given on a drive, in order. */
  driveDoses: Dose[];
  /** Days a dose may be late. Forward from the due day only. */
  maxLateDays: number | null;
  /** The value read from storage; used so an unchanged editor does not rewrite per-rule windows. */
  originalMaxLateDays?: number | null;
  /** Repeat interval in days, or null when the vaccine does not repeat. */
  repeatDays: number | null;
  /** Existing repeat/revac rule identity, when the backend has one. */
  repeatDoseCode?: string;
  /**
   * "bacterial" | "viral" | "mixed" | "unknown_review_needed". Only carried for a
   * vaccine added in this session -- toRuleDsl needs it to classify a matrix row
   * that does not exist in the original document yet. An existing row's own
   * document object is never touched, so this is undefined for it.
   */
  pathogenClass?: string;
  /** "single" | "booster". Same "new row only" scope as pathogenClass. */
  courseType?: string;
  /** "goat" | "sheep" | "both". Same "new row only" scope as pathogenClass. */
  species?: string;
  /** "all" | "breeding" | "fattening" | "non_breeding". Same "new row only" scope as pathogenClass. */
  procurementPurpose?: string;
  anchors?: Record<string, AnchorConfig>;
};

export type AnchorConfig = {
  anchorDate: string;
  reason: string;
  sourceRef: string;
  suppressBeforeAnchor: boolean;
  chainFutureFromAnchor: boolean;
  enforceAgeEligibility: boolean;
};

/** Everything "+ Add a vaccine" collects, before it becomes an EditorVaccine + a matrix row. */
export type NewVaccineInput = {
  name: string;
  code: string;
  disease: string;
  vaccineType: "live" | "killed";
  pathogenClass: "bacterial" | "viral";
  species: "goat" | "sheep" | "both";
  procurementPurpose: "all" | "breeding" | "fattening" | "non_breeding";
  courseType: "single" | "booster";
  firstDoseDays: number;
  /** Only meaningful when courseType is "booster". */
  boosterGapDays: number;
  /** null when this vaccine does not repeat. */
  repeatDays: number | null;
  maxLateDays: number;
};

/**
 * Turns what the panel collected into the same shape the rest of the editor
 * already edits. Booster follow-ups must be anchored to the accepted first
 * dose date, so manual/adult drives continue the same course instead of
 * waiting for another DOB bucket.
 */
export function newVaccineToEditor(input: NewVaccineInput): EditorVaccine {
  const code = input.code.trim();
  const kidDoses: Dose[] = [
    { offsetDays: input.firstDoseDays, triggerType: "birth_age", doseCode: `${code.toLowerCase()}_dose_1` },
  ];
  if (input.courseType === "booster") {
    kidDoses.push({
      offsetDays: input.firstDoseDays + input.boosterGapDays,
      triggerType: "birth_age",
      doseCode: `${code.toLowerCase()}_dose_2`,
    });
  }
  return {
    code,
    name: input.name.trim(),
    vaccineClass: input.vaccineType,
    disease: input.disease.trim(),
    on: true,
    kidDoses,
    driveDoses: [
      {
        offsetDays: input.firstDoseDays,
        triggerType: "manual_campaign",
        doseCode: `${code.toLowerCase()}_adult_w1`,
      },
      ...(input.courseType === "booster"
        ? [
            {
              offsetDays: input.boosterGapDays,
              triggerType: "after_previous_completion" as const,
              doseCode: `${code.toLowerCase()}_adult_w2`,
            },
          ]
        : []),
    ],
    maxLateDays: input.maxLateDays,
    repeatDays: input.repeatDays,
    pathogenClass: input.pathogenClass,
    courseType: input.courseType,
    species: input.species,
    procurementPurpose: input.procurementPurpose,
  };
}

export type ProcurementPurpose = "breeding" | "fattening";

export type ProcurementPurposePlan = {
  firstWave: string[];
  secondWaveAfterDays: number | null;
  goatSecondWave: string[];
  sheepSecondWave: string[];
};

export type ProcurementHolding = {
  warmupNoVaccinationDays: number | null;
  kidsNormalScheduleUntilWeeks: number | null;
  adultPriorVaccinationAllowed: boolean | null;
  procurementPurpose: "all" | "breeding" | "fattening" | "non_breeding";
  firstWave: string[];
  secondWaveAfterDays: number | null;
  goatSecondWave: string[];
  sheepSecondWave: string[];
  purposePlans: Record<ProcurementPurpose, ProcurementPurposePlan>;
};

export type SafetyRules = {
  liveToLiveGapDays?: number;
  liveToKilledGapDays?: number;
  killedToKilledGapDays?: number;
  kidBoosterMinGapDays?: number;
  maxVaccinesPerSession?: number;
  skipFromPregnancyMonth?: number;
  skipThroughPregnancyMonth?: number;
  postDeliveryCatchUpDays?: number;
  maxBatchingHoldDays?: number;
  deferStates: string[];
};

export type EditorPlan = {
  vaccines: EditorVaccine[];
  procurement: ProcurementHolding;
  safety: SafetyRules;
  /** "shed" | "animal" | null when the document says nothing. */
  proofMode: "shed" | "animal" | null;
};

/** Reads the document into the editor's model. */
export function fromRuleDsl(ruleDsl: unknown, proofPolicy: unknown, activeRules: ProtocolVersionRule[] = []): EditorPlan {
  const doc = asObject(ruleDsl);
  const rows = Array.isArray(doc.matrix_rows) ? (doc.matrix_rows as unknown[]) : [];
  const compat = asObject(doc.compatibility_policy);
  const preg = asObject(doc.pregnancy_policy);
  const drive = asObject(doc.drive_policy);
  const proc = asObject(doc.procurement_policy);
  const elig = asObject(doc.eligibility);
  const flatProcurementPlan = readProcurementPurposePlan(proc);
  const storedPurposePlans = asObject(proc.purpose_plans);

  return {
    vaccines: mergeAnchorConfig(mergeActiveRulesIntoEditor(rows.map(readVaccine), rows, activeRules), doc.anchor_config),
    procurement: {
      warmupNoVaccinationDays: numberOrNull(proc.warmup_no_vaccination_days),
      kidsNormalScheduleUntilWeeks: numberOrNull(proc.kids_normal_schedule_until_weeks),
      adultPriorVaccinationAllowed:
        typeof proc.adult_prior_vaccination_allowed === "boolean" ? proc.adult_prior_vaccination_allowed : null,
      procurementPurpose: readProcurementPurpose(proc.procurement_purpose),
      firstWave: stringArray(proc.first_wave),
      secondWaveAfterDays: numberOrNull(proc.second_wave_after_days),
      goatSecondWave: stringArray(proc.goat_second_wave),
      sheepSecondWave: stringArray(proc.sheep_second_wave),
      purposePlans: {
        breeding: readProcurementPurposePlan(asObject(storedPurposePlans.breeding), flatProcurementPlan),
        fattening: readProcurementPurposePlan(asObject(storedPurposePlans.fattening), flatProcurementPlan),
      },
    },
    safety: {
      liveToLiveGapDays: numberOrUndefined(compat.live_to_live_gap_days),
      liveToKilledGapDays: numberOrUndefined(compat.live_to_killed_gap_days),
      killedToKilledGapDays: numberOrUndefined(compat.killed_to_killed_gap_days),
      kidBoosterMinGapDays: numberOrUndefined(compat.kid_booster_min_gap_days),
      maxVaccinesPerSession: numberOrUndefined(compat.max_vaccines_per_combo_session),
      skipFromPregnancyMonth: numberOrUndefined(preg.skip_from_pregnancy_month),
      skipThroughPregnancyMonth: numberOrUndefined(preg.skip_through_pregnancy_month),
      postDeliveryCatchUpDays: numberOrUndefined(preg.post_delivery_catch_up_days),
      maxBatchingHoldDays: numberOrUndefined(drive.max_batching_hold_days),
      deferStates: Array.isArray(elig.defer_states) ? (elig.defer_states as string[]) : [],
    },
    proofMode: readProofMode(proofPolicy),
  };
}

function readVaccine(row: unknown): EditorVaccine {
  const r = asObject(row);
  const vaccine = asObject(r.vaccine);
  const live = Array.isArray(r.schedule) ? (r.schedule as ScheduleRule[]) : [];
  const parked = Array.isArray(r.parked_schedule) ? (r.parked_schedule as ScheduleRule[]) : [];

  // A switched-off vaccine keeps its course on parked_schedule. Reading only
  // `schedule` made the editor believe it had no doses: switching it back on then
  // demanded a dose the farm had already chosen, wrote that dose's timing onto the
  // first parked rule, resurrected the rest invisibly, and -- because repeatDays
  // read as null -- deleted the repeat cadence entirely. The parked course is what
  // the editor edits; `on` is still decided by the LIVE schedule.
  const schedule = live.length > 0 ? live : parked;
  const repeats = schedule.filter((s) => s.repeat && s.repeat !== "none");
  const firsts = schedule.filter((s) => !s.repeat || s.repeat === "none");
  const maxLateDays = numberOrNull(firsts[0]?.due_window_days ?? repeats[0]?.due_window_days);

  return {
    code: String(vaccine.code ?? r.row_id ?? ""),
    name: String(vaccine.name ?? vaccine.code ?? ""),
    vaccineClass: String(vaccine.type ?? ""),
    disease: String(vaccine.disease ?? ""),
    on: live.length > 0,
    kidDoses: firsts.filter((s) => s.trigger_type === "birth_age").map(toDose),
    driveDoses: firsts.filter((s) => s.trigger_type !== "birth_age").map(toDose),
    // Every dose in a course carries the same window in this document; the
    // editor shows one value rather than pretending they are independent.
    maxLateDays,
    originalMaxLateDays: maxLateDays,
    repeatDays: numberOrNull(repeats[0]?.offset_days),
    repeatDoseCode: typeof repeats[0]?.dose_code === "string" ? repeats[0]?.dose_code : undefined,
    pathogenClass: typeof vaccine.pathogen_class === "string" ? vaccine.pathogen_class : undefined,
    courseType: typeof vaccine.course_type === "string" ? vaccine.course_type : undefined,
    anchors: {},
  };
}

function toDose(rule: ScheduleRule): Dose {
  return {
    offsetDays: Number(rule.offset_days ?? 0),
    triggerType: String(rule.trigger_type ?? ""),
    doseCode: String(rule.dose_code ?? ""),
  };
}

function mergeActiveRulesIntoEditor(
  vaccines: EditorVaccine[],
  rows: unknown[],
  activeRules: ProtocolVersionRule[],
): EditorVaccine[] {
  if (activeRules.length === 0) return vaccines;
  const byDose = vaccineLookupByDose(rows);
  const byCode = new Map<string, EditorVaccine>(
    vaccines.map((v) => [normaliseVaccineName(v.code), { ...v, kidDoses: [...v.kidDoses], driveDoses: [...v.driveDoses] }]),
  );
  const reset = new Set<string>();
  const activeDoseOwners = new Map<string, string>();
  for (const rule of activeRules) {
    const eligibilityVaccine = vaccineFromEligibility(rule.eligibility_json);
    const vaccine = eligibilityVaccine.code
      ? eligibilityVaccine
      : byDose.get(String(rule.dose_code).trim().toLowerCase()) ?? eligibilityVaccine;
    if (!vaccine.code) continue;
    const key = normaliseVaccineName(vaccine.code);
    const doseKey = String(rule.dose_code ?? "").trim().toLowerCase();
    if (doseKey) activeDoseOwners.set(doseKey, key);
    const current =
      byCode.get(key) ??
      ({
        code: vaccine.code,
        name: vaccine.name || vaccine.code,
        vaccineClass: vaccine.type,
        disease: vaccine.disease,
        on: true,
        kidDoses: [],
        driveDoses: [],
        maxLateDays: numberOrNull(rule.due_window_days),
        originalMaxLateDays: numberOrNull(rule.due_window_days),
        repeatDays: null,
        repeatDoseCode: undefined,
      } satisfies EditorVaccine);
    current.code = vaccine.code;
    current.name = vaccine.name || current.name;
    current.vaccineClass = vaccine.type || current.vaccineClass;
    current.disease = vaccine.disease || current.disease;
    current.on = true;
    if (!reset.has(key)) {
      current.kidDoses = [];
      current.driveDoses = [];
      current.repeatDays = null;
      current.repeatDoseCode = undefined;
      reset.add(key);
    }
    if (rule.repeat && rule.repeat !== "none") {
      current.repeatDays = numberOrNull(rule.offset_days);
      current.repeatDoseCode = String(rule.dose_code ?? "");
    } else {
      const dose = {
        offsetDays: Number(rule.offset_days ?? 0),
        triggerType: String(rule.trigger_type ?? ""),
        doseCode: String(rule.dose_code ?? ""),
      };
      if (dose.triggerType === "birth_age") current.kidDoses.push(dose);
      else current.driveDoses.push(dose);
    }
    byCode.set(key, current);
  }
  const merged = [...byCode.entries()].map(([key, vaccine]) => {
    if (reset.has(key)) return vaccine;
    const kidDoses = vaccine.kidDoses.filter((dose) => !isOwnedByAnotherActiveVaccine(activeDoseOwners, dose.doseCode, key));
    const driveDoses = vaccine.driveDoses.filter((dose) => !isOwnedByAnotherActiveVaccine(activeDoseOwners, dose.doseCode, key));
    const repeatClaimedElsewhere =
      vaccine.repeatDoseCode && isOwnedByAnotherActiveVaccine(activeDoseOwners, vaccine.repeatDoseCode, key);
    const next = {
      ...vaccine,
      kidDoses,
      driveDoses,
      repeatDays: repeatClaimedElsewhere ? null : vaccine.repeatDays,
      repeatDoseCode: repeatClaimedElsewhere ? undefined : vaccine.repeatDoseCode,
    };
    if (kidDoses.length === 0 && driveDoses.length === 0 && next.repeatDays === null) next.on = false;
    return next;
  });
  return [...merged.filter((v) => v.on), ...merged.filter((v) => !v.on)];
}

function isOwnedByAnotherActiveVaccine(activeDoseOwners: ReadonlyMap<string, string>, doseCode: string | undefined, vaccineKey: string): boolean {
  const owner = activeDoseOwners.get(String(doseCode ?? "").trim().toLowerCase());
  return Boolean(owner && owner !== vaccineKey);
}

function vaccineLookupByDose(rows: unknown[]): ReadonlyMap<string, { code: string; name: string; type: string; disease: string }> {
  const out = new Map<string, { code: string; name: string; type: string; disease: string }>();
  for (const row of rows) {
    const r = asObject(row);
    const vaccine = asObject(r.vaccine);
    const code = String(vaccine.code ?? r.row_id ?? "");
    const value = {
      code,
      name: String(vaccine.name ?? code),
      type: String(vaccine.type ?? ""),
      disease: String(vaccine.disease ?? ""),
    };
    const schedule = Array.isArray(r.schedule) ? (r.schedule as ScheduleRule[]) : [];
    for (const rule of schedule) {
      const dose = String(rule.dose_code ?? "").trim().toLowerCase();
      if (dose) out.set(dose, value);
    }
  }
  return out;
}

function vaccineFromEligibility(value: unknown): { code: string; name: string; type: string; disease: string } {
  const eligibility = asObject(value);
  const vaccine = asObject(eligibility.vaccine);
  const code = String(vaccine.code ?? "");
  return {
    code,
    name: String(vaccine.name ?? code),
    type: String(vaccine.type ?? ""),
    disease: String(vaccine.disease ?? ""),
  };
}

/**
 * Writes the editor's changes back onto a DEEP COPY of the original document.
 *
 * Only the fields the editor owns are touched. Turning a vaccine off empties its
 * schedule but keeps the row, so the farm can see what it decided not to give
 * and can turn it back on without re-authoring it.
 */
export function toRuleDsl(original: unknown, plan: EditorPlan): unknown {
  const doc = sanitizeRuleDslForSave(structuredClone(asObject(original)));
  const byCode = new Map(plan.vaccines.map((v) => [v.code, v]));

  const rows = Array.isArray(doc.matrix_rows) ? (doc.matrix_rows as unknown[]) : [];
  const seenCodes = new Set<string>();
  const editedRows = rows.map((row) => {
    const r = asObject(row);
    const code = String(asObject(r.vaccine).code ?? r.row_id ?? "");
    seenCodes.add(code);
    const edited = byCode.get(code);
    if (!edited) return r;
    // Switching a vaccine OFF must not destroy it. dose_amount, dose_unit,
    // route_site, catch_up and course_lapse_policy live ONLY on these rules, and
    // nothing in the editor carries them -- so emptying the schedule threw away
    // clinical values the farm chose, and switching the vaccine back on later
    // rebuilt doses that had lost them. The rules are parked instead, and taken
    // back out when the vaccine returns to the plan.
    //
    // "Off" still means an EMPTY schedule, which is what generation reads; the
    // parked copy is inert to it.
    const parked = Array.isArray(r.parked_schedule) ? (r.parked_schedule as ScheduleRule[]) : [];
    const live = Array.isArray(r.schedule) ? (r.schedule as ScheduleRule[]) : [];
    // The same source either way -- the live rules, or the parked ones when the vaccine
    // was already off -- and the SAME edits applied to them. Parking the untouched
    // original instead lost whatever the farm had just changed: set a first dose to five
    // months, switch the vaccine off, save, and the screen still read five months while
    // the parked rules had silently reverted to four weeks. Nothing warned, and the loss
    // only surfaced when the vaccine came back on.
    const source = live.length > 0 ? live : parked;
    const edits = applyEdits(source, edited, code);
    if (edited.on) {
      r.schedule = edits;
      delete r.parked_schedule;
    } else {
      if (edits.length > 0) r.parked_schedule = edits;
      r.schedule = [];
    }
    return r;
  });

  // A vaccine the plan carries that the original document never had is one added
  // in this editing session via "+ Add a vaccine". It gets a brand new matrix
  // row -- there is no existing row to edit onto.
  //
  // row_id has to be unique across the whole matrix or publish rejects the draft, and it is
  // derived from the short code rather than being the short code: an existing "ET+TT" carries
  // row_id "et_tt", so a newly authored "ET_TT" passes the editor's short-code check and still
  // collides here. Every id already spoken for -- by an existing row or by an earlier addition
  // in this same session -- is therefore reserved as the ids are handed out.
  const takenRowIds = new Set(
    editedRows.map((row) => String(asObject(row).row_id ?? "").toLowerCase()).filter(Boolean),
  );
  const newRows = plan.vaccines
    .filter((v) => !seenCodes.has(v.code))
    .map((v) => {
      const row = buildNewMatrixRow(v, doc, takenRowIds, plan.procurement.procurementPurpose);
      takenRowIds.add(String(row.row_id));
      return row;
    });
  doc.matrix_rows = [...editedRows, ...newRows];

  // The flat top-level schedule is the union of every row's schedule, and the
  // generator reads it. Rebuilt from the rows so the two can never disagree.
  doc.schedule = (doc.matrix_rows as unknown[]).flatMap((row) => {
    const s = asObject(row).schedule;
    return Array.isArray(s) ? s : [];
  });
  writeAnchorConfig(doc, plan);

  const proc = asObject(doc.procurement_policy);
  if (plan.procurement.warmupNoVaccinationDays !== null) {
    proc.warmup_no_vaccination_days = plan.procurement.warmupNoVaccinationDays;
  }
  if (plan.procurement.kidsNormalScheduleUntilWeeks !== null) {
    proc.kids_normal_schedule_until_weeks = plan.procurement.kidsNormalScheduleUntilWeeks;
  }
  if (plan.procurement.adultPriorVaccinationAllowed !== null) {
    proc.adult_prior_vaccination_allowed = plan.procurement.adultPriorVaccinationAllowed;
  }
  const activeVaccines = plan.vaccines.filter((v) => v.on);
  const sanitizeProcurementWave = (items: string[]) => filterActiveProcurementVaccines(items, activeVaccines);
  proc.procurement_purpose = plan.procurement.procurementPurpose;
  const purposePlansPayload = {
    breeding: {
      first_wave: sanitizeProcurementWave(plan.procurement.purposePlans.breeding.firstWave),
      second_wave_after_days: plan.procurement.purposePlans.breeding.secondWaveAfterDays,
      goat_second_wave: sanitizeProcurementWave(plan.procurement.purposePlans.breeding.goatSecondWave),
      sheep_second_wave: sanitizeProcurementWave(plan.procurement.purposePlans.breeding.sheepSecondWave),
    },
    fattening: {
      first_wave: sanitizeProcurementWave(plan.procurement.purposePlans.fattening.firstWave),
      second_wave_after_days: plan.procurement.purposePlans.fattening.secondWaveAfterDays,
      goat_second_wave: sanitizeProcurementWave(plan.procurement.purposePlans.fattening.goatSecondWave),
      sheep_second_wave: sanitizeProcurementWave(plan.procurement.purposePlans.fattening.sheepSecondWave),
    },
  };
  proc.purpose_plans = purposePlansPayload;
  proc.first_wave = purposePlansPayload.breeding.first_wave;
  if (plan.procurement.purposePlans.breeding.secondWaveAfterDays !== null) {
    proc.second_wave_after_days = plan.procurement.purposePlans.breeding.secondWaveAfterDays;
  }
  proc.goat_second_wave = purposePlansPayload.breeding.goat_second_wave;
  proc.sheep_second_wave = purposePlansPayload.breeding.sheep_second_wave;
  doc.procurement_policy = proc;

  return doc;
}

function mergeAnchorConfig(vaccines: EditorVaccine[], raw: unknown): EditorVaccine[] {
  const config = asObject(raw);
  const rules = Array.isArray(config.rules) ? config.rules : [];
  if (rules.length === 0) return vaccines.map((v) => ({ ...v, anchors: v.anchors ?? {} }));
  const byVaccine = new Map(vaccines.map((v) => [normaliseVaccineName(v.code), { ...v, anchors: { ...(v.anchors ?? {}) } }]));
  for (const item of rules) {
    const r = asObject(item);
    const vaccineCode = String(r.vaccine_code ?? "");
    const doseCode = String(r.dose_code ?? "");
    const anchorDate = String(r.anchor_date ?? "");
    if (!vaccineCode || !doseCode || !anchorDate) continue;
    const vaccine = byVaccine.get(normaliseVaccineName(vaccineCode));
    if (!vaccine) continue;
    vaccine.anchors = {
      ...(vaccine.anchors ?? {}),
      [doseCode]: {
        anchorDate,
        reason: String(r.reason ?? "Anchor/base date for this vaccine rule"),
        sourceRef: String(r.source_ref ?? ""),
        suppressBeforeAnchor: r.suppress_before_anchor !== false,
        chainFutureFromAnchor: r.chain_future_from_anchor !== false,
        enforceAgeEligibility: r.enforce_age_eligibility !== false,
      },
    };
  }
  return vaccines.map((v) => byVaccine.get(normaliseVaccineName(v.code)) ?? { ...v, anchors: v.anchors ?? {} });
}

function writeAnchorConfig(doc: Record<string, unknown>, plan: EditorPlan) {
  const rules: Record<string, unknown>[] = [];
  for (const vaccine of plan.vaccines) {
    for (const [doseCode, anchor] of Object.entries(vaccine.anchors ?? {})) {
      if (!anchor.anchorDate) continue;
      const item: Record<string, unknown> = {
        vaccine_code: vaccine.code,
        dose_code: doseCode,
        anchor_date: anchor.anchorDate,
        scope_type: "tenant",
        suppress_before_anchor: anchor.suppressBeforeAnchor,
        chain_future_from_anchor: anchor.chainFutureFromAnchor,
        enforce_age_eligibility: anchor.enforceAgeEligibility,
        reason: anchor.reason.trim() || "Anchor/base date for this vaccine rule",
      };
      if (anchor.sourceRef.trim()) item.source_ref = anchor.sourceRef.trim();
      rules.push(item);
    }
  }
  if (rules.length === 0) {
    delete doc.anchor_config;
    return;
  }
  doc.anchor_config = { rules };
}

/**
 * Draft authoring must not preserve legacy/import-only keys that publish now
 * rejects. Config-level anchor/base dates are the exception: they live in a
 * validated anchor_config block until publish applies them through the anchor
 * service.
 */
export function sanitizeRuleDslForSave(ruleDsl: unknown): Record<string, unknown> {
  const doc = structuredClone(asObject(ruleDsl));
  delete doc.notes;
  return doc;
}

function filterActiveProcurementVaccines(items: string[], vaccines: EditorVaccine[]): string[] {
  const active = new Map<string, string>();
  for (const vaccine of vaccines) {
    active.set(normaliseVaccineName(vaccine.code), vaccine.name);
    active.set(normaliseVaccineName(vaccine.name), vaccine.name);
  }
  const out: string[] = [];
  const seen = new Set<string>();
  for (const item of items) {
    const name = active.get(normaliseVaccineName(item));
    if (!name) continue;
    const key = normaliseVaccineName(name);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(name);
  }
  return out;
}

function normaliseVaccineName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, "");
}

function readProcurementPurposePlan(value: Record<string, unknown>, fallback?: ProcurementPurposePlan): ProcurementPurposePlan {
  return {
    firstWave: stringArray(value.first_wave ?? fallback?.firstWave),
    secondWaveAfterDays: numberOrNull(value.second_wave_after_days ?? fallback?.secondWaveAfterDays),
    goatSecondWave: stringArray(value.goat_second_wave ?? fallback?.goatSecondWave),
    sheepSecondWave: stringArray(value.sheep_second_wave ?? fallback?.sheepSecondWave),
  };
}

/**
 * A schedule rule for a dose the plan did not have before.
 *
 * Built from the row's own existing rules where there are any, so a new dose
 * inherits this vaccine's dose amount, vial size, route and policies rather than
 * this file inventing clinical values it has no business choosing. With nothing
 * to copy, only the fields the editor genuinely knows are set.
 */
function newRule(
  template: ScheduleRule | undefined,
  code: string,
  kind: "kid" | "drive" | "repeat",
  offsetDays: number,
  sequence: number,
  doseCode?: string,
): ScheduleRule {
  const base: Record<string, unknown> = template ? { ...(template as Record<string, unknown>) } : {};
  // With no sibling dose to copy from, publish's own required fields
  // (dose_amount, dose_unit, route_site, course_lapse_policy) would otherwise be
  // silently absent -- exactly the values a template row would have carried.
  // These are ordinary, reviewable defaults, not invented clinical judgement:
  // the operator app already prompts for the actual amount given at the point
  // of injection, and course_lapse_policy routes an unresolved course to human
  // review rather than resolving it silently either way.
  if (!template) {
    base.dose_amount = 1;
    base.dose_unit = "ml";
    base.route_site = "subcutaneous";
    base.course_lapse_policy = "pc_review";
  }
  base.dose_code = doseCode || `${code.toLowerCase()}_${kind}_${sequence}`;
  base.source_dose_code = base.dose_code;
  base.sequence = sequence;
  base.offset_days = offsetDays;
  base.trigger_type =
    kind === "kid" ? "birth_age" : kind === "drive" ? "manual_campaign" : "after_previous_completion";
  base.repeat = kind === "repeat" ? "every_n_days" : "none";
  base.catch_up = kind === "repeat" ? "next_cycle" : "immediate";
  base.min_gap_days = kind === "repeat" ? offsetDays : 0;
  return base as ScheduleRule;
}

/** Applies edited timings onto the row's existing rules, preserving every other key. */
function applyEdits(schedule: ScheduleRule[], edited: EditorVaccine, code: string): ScheduleRule[] {
  const kid = [...edited.kidDoses];
  const drive = [...edited.driveDoses];
  const template = schedule[0];
  const existingRepeat = schedule.find((r) => r.repeat && r.repeat !== "none");
  let nextSequence = schedule.reduce((max, r) => Math.max(max, Number(r.sequence ?? 0)), 0);
  const maxLateDays = edited.maxLateDays;
  const deadlineChanged =
    maxLateDays !== null && (edited.originalMaxLateDays === undefined || maxLateDays !== edited.originalMaxLateDays);

  const kept = schedule.map((rule) => {
    const next: ScheduleRule = { ...rule };
    if (rule.repeat && rule.repeat !== "none") {
      if (edited.repeatDays !== null) {
        next.offset_days = edited.repeatDays;
        // A repeat may not come round sooner than its own minimum spacing.
        next.min_gap_days = edited.repeatDays;
      }
    } else if (rule.trigger_type === "birth_age") {
      const dose = kid.shift();
      if (dose) next.offset_days = dose.offsetDays;
    } else {
      const dose = drive.shift();
      if (dose) next.offset_days = dose.offsetDays;
    }
    if (deadlineChanged) {
      next.due_window_days = maxLateDays;
      next.max_delay_days = maxLateDays;
    }
    return next;
  });

  // Doses the editor added have no rule to update, so they are appended. The
  // leftovers in `kid`/`drive` are exactly those: every existing rule shifted
  // one off the front above.
  const added: ScheduleRule[] = [];
  for (const dose of kid) {
    nextSequence += 1;
    added.push(withWindow(newRule(template, code, "kid", dose.offsetDays, nextSequence, dose.doseCode), edited));
  }
  for (const dose of drive) {
    nextSequence += 1;
    added.push(withWindow(newRule(template, code, "drive", dose.offsetDays, nextSequence, dose.doseCode), edited));
  }
  if (edited.repeatDays !== null && !existingRepeat) {
    nextSequence += 1;
    added.push(withWindow(newRule(template, code, "repeat", edited.repeatDays, nextSequence), edited));
  }

  // A repeat that was removed in the editor must not survive in the document.
  const surviving = edited.repeatDays === null ? kept.filter((r) => !r.repeat || r.repeat === "none") : kept;
  return [...surviving, ...added];
}

function withWindow(rule: ScheduleRule, edited: EditorVaccine): ScheduleRule {
  if (edited.maxLateDays === null) return rule;
  return { ...rule, due_window_days: edited.maxLateDays, max_delay_days: edited.maxLateDays };
}

/**
 * A whole new `matrix_rows` entry for a vaccine "+ Add a vaccine" created.
 *
 * Eligibility is not invented from nothing: `sex`, `breed`, `lifecycle`,
 * `health`, `reproductive`, `exclude_reproductive_states` and `defer_states` are
 * copied from the plan's own top-level eligibility -- the same clinical
 * judgement (who is deferred, what counts as pregnant-late) that already governs
 * every other vaccine in this plan, not a second, competing set of defaults.
 * Only `species` and `animal_stage` are this row's own, because they are the one
 * thing the panel actually asked the author to decide.
 */
/**
 * A matrix row id that no other row in the document already holds.
 *
 * Publish rejects a matrix whose row ids repeat, and the id is derived from the short code, so
 * two codes that differ only in punctuation ("ET+TT" and "ET_TT") derive the same id. Rather
 * than refuse the vaccine over a detail the author cannot see, the id is suffixed until it is
 * free -- the short code the author typed is preserved untouched as vaccine.code.
 */
function uniqueRowId(code: string, taken: ReadonlySet<string>): string {
  const base = code.toLowerCase();
  if (!taken.has(base)) return base;
  for (let suffix = 2; ; suffix += 1) {
    const candidate = `${base}_${suffix}`;
    if (!taken.has(candidate)) return candidate;
  }
}

function buildNewMatrixRow(
  v: EditorVaccine,
  doc: Record<string, unknown>,
  takenRowIds: ReadonlySet<string> = new Set(),
  defaultProcurementPurpose: "all" | "breeding" | "fattening" | "non_breeding" = "all",
): Record<string, unknown> {
  const topEligibility = asObject(doc.eligibility);
  const species = v.species === "goat" ? ["goat"] : v.species === "sheep" ? ["sheep"] : ["goat", "sheep"];

  const rowEligibility: Record<string, unknown> = {
    animal_stage: "all",
    species,
    sex: topEligibility.sex ?? "all",
    breed: topEligibility.breed ?? "all",
    lifecycle: topEligibility.lifecycle ?? "alive",
    health: topEligibility.health ?? "any",
    reproductive: topEligibility.reproductive ?? "any",
    exclude_reproductive_states: topEligibility.exclude_reproductive_states ?? ["pregnant_late"],
    defer_states: topEligibility.defer_states ?? [
      "sick",
      "under_treatment",
      "recovering",
      "icu",
      "quarantine",
    ],
  };
  const procurementPurpose =
    v.procurementPurpose === undefined || v.procurementPurpose === null
      ? defaultProcurementPurpose
      : v.procurementPurpose;
  if (procurementPurpose !== "all") {
    rowEligibility.procurement_purpose = [procurementPurpose];
  }

  const schedule = applyEdits([], v, v.code);

  return {
    row_id: uniqueRowId(v.code, takenRowIds),
    vaccine: {
      code: v.code,
      name: v.name,
      type: v.vaccineClass,
      disease: v.disease,
      pathogen_class: v.pathogenClass ?? "unknown_review_needed",
      course_type: v.courseType ?? "single",
      compatibility_group: v.code,
    },
    eligibility: rowEligibility,
    schedule,
  };
}

/**
 * Whether proof is one clip per shed or one per animal.
 *
 * Read from subject_scope / proof_mode, the fields that actually carry it, not
 * by searching the serialised policy for the word "shed" -- that matched
 * `expected_subjects` too and would have reported "shed" for a per-animal
 * policy that merely mentioned one.
 */
function readProofMode(proofPolicy: unknown): "shed" | "animal" | null {
  const policy = asObject(proofPolicy);
  const scope = typeof policy.subject_scope === "string" ? policy.subject_scope.toLowerCase() : "";
  if (scope === "shed") return "shed";
  if (scope === "animal" || scope === "goat") return "animal";
  const mode = typeof policy.proof_mode === "string" ? policy.proof_mode.toLowerCase() : "";
  if (mode.includes("shed")) return "shed";
  if (mode.includes("animal") || mode.includes("goat")) return "animal";
  return null;
}

function asObject(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? { ...(value as Record<string, unknown>) } : {};
}

function numberOrNull(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function numberOrUndefined(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function readProcurementPurpose(value: unknown): "all" | "breeding" | "fattening" | "non_breeding" {
  return value === "breeding" || value === "fattening" || value === "non_breeding" ? value : "all";
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}
