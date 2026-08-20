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
  /** Repeat interval in days, or null when the vaccine does not repeat. */
  repeatDays: number | null;
};

export type ProcurementHolding = {
  warmupNoVaccinationDays: number | null;
  kidsNormalScheduleUntilWeeks: number | null;
  adultPriorVaccinationAllowed: boolean | null;
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
export function fromRuleDsl(ruleDsl: unknown, proofPolicy: unknown): EditorPlan {
  const doc = asObject(ruleDsl);
  const rows = Array.isArray(doc.matrix_rows) ? (doc.matrix_rows as unknown[]) : [];
  const compat = asObject(doc.compatibility_policy);
  const preg = asObject(doc.pregnancy_policy);
  const drive = asObject(doc.drive_policy);
  const proc = asObject(doc.procurement_policy);
  const elig = asObject(doc.eligibility);

  return {
    vaccines: rows.map(readVaccine),
    procurement: {
      warmupNoVaccinationDays: numberOrNull(proc.warmup_no_vaccination_days),
      kidsNormalScheduleUntilWeeks: numberOrNull(proc.kids_normal_schedule_until_weeks),
      adultPriorVaccinationAllowed:
        typeof proc.adult_prior_vaccination_allowed === "boolean" ? proc.adult_prior_vaccination_allowed : null,
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
  const schedule = Array.isArray(r.schedule) ? (r.schedule as ScheduleRule[]) : [];
  const repeats = schedule.filter((s) => s.repeat && s.repeat !== "none");
  const firsts = schedule.filter((s) => !s.repeat || s.repeat === "none");

  return {
    code: String(vaccine.code ?? r.row_id ?? ""),
    name: String(vaccine.name ?? vaccine.code ?? ""),
    vaccineClass: String(vaccine.type ?? ""),
    disease: String(vaccine.disease ?? ""),
    on: schedule.length > 0,
    kidDoses: firsts.filter((s) => s.trigger_type === "birth_age").map(toDose),
    driveDoses: firsts.filter((s) => s.trigger_type !== "birth_age").map(toDose),
    // Every dose in a course carries the same window in this document; the
    // editor shows one value rather than pretending they are independent.
    maxLateDays: numberOrNull(firsts[0]?.due_window_days ?? repeats[0]?.due_window_days),
    repeatDays: numberOrNull(repeats[0]?.offset_days),
  };
}

function toDose(rule: ScheduleRule): Dose {
  return {
    offsetDays: Number(rule.offset_days ?? 0),
    triggerType: String(rule.trigger_type ?? ""),
    doseCode: String(rule.dose_code ?? ""),
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
  const doc = structuredClone(asObject(original));
  const byCode = new Map(plan.vaccines.map((v) => [v.code, v]));

  const rows = Array.isArray(doc.matrix_rows) ? (doc.matrix_rows as unknown[]) : [];
  doc.matrix_rows = rows.map((row) => {
    const r = asObject(row);
    const code = String(asObject(r.vaccine).code ?? r.row_id ?? "");
    const edited = byCode.get(code);
    if (!edited) return r;
    r.schedule = edited.on ? applyEdits(Array.isArray(r.schedule) ? (r.schedule as ScheduleRule[]) : [], edited) : [];
    return r;
  });

  // The flat top-level schedule is the union of every row's schedule, and the
  // generator reads it. Rebuilt from the rows so the two can never disagree.
  doc.schedule = (doc.matrix_rows as unknown[]).flatMap((row) => {
    const s = asObject(row).schedule;
    return Array.isArray(s) ? s : [];
  });

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
  doc.procurement_policy = proc;

  return doc;
}

/** Applies edited timings onto the row's existing rules, preserving every other key. */
function applyEdits(schedule: ScheduleRule[], edited: EditorVaccine): ScheduleRule[] {
  const kid = [...edited.kidDoses];
  const drive = [...edited.driveDoses];

  return schedule.map((rule) => {
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
    if (edited.maxLateDays !== null) {
      next.due_window_days = edited.maxLateDays;
      next.max_delay_days = edited.maxLateDays;
    }
    return next;
  });
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
