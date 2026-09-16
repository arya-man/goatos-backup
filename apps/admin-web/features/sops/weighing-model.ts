// WEIGHING SOP (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md).
//
// Pure model of `form_dsl.weighing`: the rules a weighing task is planned on and runs under,
// authored on /weighing/sops -- the capture modes the planner may pick, the default cap per day,
// whether the evening-before feed & water removal is required / optional / off, the removal
// card's instruction, the two proof slots' wording, the questions the removal operator answers
// per pen, and the lump-sum video window. Parses the backend document into editor rows and emits
// it back byte-faithfully, so a round-trip with no edits publishes the SAME document. The backend
// (weighing/domain.ValidateWeighingSOP) is the authority on what is valid; this file only shapes
// and pre-checks so the author gets the message beside the field.

export const WEIGHING_SCHEMA_VERSION = "goatos.sop-weighing.v1";

export type WeighingMode = "individual_animal" | "per_shed_partition";
export const WEIGHING_MODES: WeighingMode[] = ["individual_animal", "per_shed_partition"];

export type RemovalMode = "required" | "optional" | "off";
export const REMOVAL_MODES: RemovalMode[] = ["required", "optional", "off"];

// The removal card's captures are the author's (second 2026-09-15 decision): any number of
// slots, each a live-camera video, a photo or either, compulsory or optional. The seed's two are
// feed_video and water_video. At least one slot must be compulsory.
export type RemovalProofKind = "video" | "photo" | "either";
export const REMOVAL_PROOF_KINDS: RemovalProofKind[] = ["video", "photo", "either"];
export const MAX_REMOVAL_PROOF_SLOTS = 8;

export type WeighingQuestionKind = "choice" | "multi" | "text" | "number";

export type WeighingOptionRow = { value: string; label: string };

export type WeighingQuestionRow = {
  id: string; // editor row id
  key: string; // question id on the wire
  kind: WeighingQuestionKind;
  title: string;
  hint: string;
  required: boolean;
  options: WeighingOptionRow[];
  allowOther: boolean;
  min: string;
  max: string;
  unit: string;
  onlyIfQuestion: string;
  onlyIfValue: string;
};

export type RemovalProofRow = { id: string; key: string; title: string; hint: string; kind: RemovalProofKind; required: boolean };

export type WeighingRows = {
  modes: WeighingMode[];
  defaultCapPerDay: string;
  removalMode: RemovalMode;
  removalInstruction: string;
  /** "HH:MM" Asia/Kolkata; blank = the farm-wide removal evening (feed_water_removal_config). */
  removalCutoffTime: string;
  removalProofs: RemovalProofRow[];
  removalQuestions: WeighingQuestionRow[];
  individualVideoRequired: boolean;
  lumpSumVideoMin: string;
  lumpSumVideoMax: string;
  /**
   * The Weights / ADG Analytics pages' window (maintainer request 2026-09-16): the period the pages
   * open on -- a fixed date or the last N days -- and the earliest day their calendars offer.
   */
  weightsFromMode: WeightsFromMode;
  weightsFromDate: string;
  weightsFromDays: string;
  weightsFromWeeks: string;
  weightsEarliestDate: string;
};

export type WeightsFromMode = "fixed_date" | "rolling_days" | "rolling_weeks";

// The seeded window, which is what the pages hardcoded before the block existed.
export const SEEDED_WEIGHTS_FROM_DATE = "2026-08-03";
export const SEEDED_WEIGHTS_EARLIEST_DATE = "2026-07-05";
const ISO_DAY = /^\d{4}-\d{2}-\d{2}$/;

// The proof policy's ceiling (weighing/domain.MaxShedProofArtifacts); the backend refuses more.
export const LUMP_SUM_VIDEO_CEILING = 5;

function str(v: unknown, fallback = ""): string {
  return typeof v === "string" ? v : fallback;
}
function num(v: unknown): string {
  return typeof v === "number" && Number.isFinite(v) ? String(v) : "";
}
function obj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

let seq = 0;
export function newRowId(prefix = "wq"): string {
  seq += 1;
  return `${prefix}-${Date.now().toString(36)}-${seq}`;
}

export function blankQuestion(kind: WeighingQuestionKind = "choice"): WeighingQuestionRow {
  return {
    id: newRowId(), key: "", kind, title: "", hint: "", required: true,
    options: kind === "choice" ? [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }] : [],
    allowOther: false, min: "", max: "", unit: "", onlyIfQuestion: "", onlyIfValue: "",
  };
}

// slugKey derives a stable key from a title for NEW rows (an existing key is never rewritten).
export function slugKey(title: string, taken: Set<string>, fallback = "question"): string {
  const base = title.toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "").replace(/^[^a-z]+/, "").slice(0, 40) || fallback;
  let key = base;
  let n = 2;
  while (taken.has(key)) {
    key = `${base}_${n}`;
    n += 1;
  }
  return key;
}

function parseQuestion(rq: unknown): WeighingQuestionRow[] {
  const q = obj(rq);
  if (!q) return [];
  const onlyIf = obj(q["only_if"]);
  const options = Array.isArray(q["options"]) ? q["options"].flatMap((o) => { const oo = obj(o); return oo ? [{ value: str(oo["value"]), label: str(oo["label"]) }] : []; }) : [];
  return [
    {
      id: newRowId(),
      key: str(q["id"]),
      kind: (str(q["kind"], "choice") || "choice") as WeighingQuestionKind,
      title: str(q["title"]),
      hint: str(q["hint"]),
      required: q["required"] === true,
      options,
      allowOther: q["allow_other"] === true,
      min: num(q["min"]),
      max: num(q["max"]),
      unit: str(q["unit"]),
      onlyIfQuestion: onlyIf ? str(onlyIf["question_id"]) : "",
      onlyIfValue: onlyIf ? str(onlyIf["value"]) : "",
    },
  ];
}

export function parseWeighing(formDsl: unknown): WeighingRows | null {
  const dsl = obj(formDsl);
  const w = dsl ? obj(dsl["weighing"]) : null;
  if (!w) return null;
  const planning = obj(w["planning"]) ?? {};
  const removal = obj(w["feed_water_removal"]) ?? {};
  const capture = obj(w["capture"]) ?? {};
  const individual = obj(capture["individual"]) ?? {};
  const lumpSum = obj(capture["lump_sum"]) ?? {};
  // Absent on a document published before the block: the seed, as the backend reads it.
  const weights = obj(w["weights_pages"]) ?? {};
  const weightsFromMode: WeightsFromMode = weights["default_from_mode"] === "rolling_weeks" ? "rolling_weeks" : weights["default_from_mode"] === "rolling_days" ? "rolling_days" : "fixed_date";
  const modes = Array.isArray(planning["modes"]) ? planning["modes"].filter((m): m is WeighingMode => m === "individual_animal" || m === "per_shed_partition") : [];
  const proofsRaw = Array.isArray(removal["proofs"]) ? removal["proofs"] : [];
  const proofs: RemovalProofRow[] = proofsRaw.flatMap((raw) => {
    const p = obj(raw);
    if (!p) return [];
    const kind = str(p["kind"], "video");
    return [{
      id: newRowId("ws"),
      key: str(p["key"]),
      title: str(p["title"]),
      hint: str(p["hint"]),
      kind: (kind === "photo" || kind === "either" ? kind : "video") as RemovalProofKind,
      // A slot published before the flag existed is compulsory (the shape those documents meant).
      required: p["required"] !== false,
    }];
  });
  const questions = Array.isArray(removal["questions"]) ? removal["questions"].flatMap(parseQuestion) : [];
  return {
    modes,
    defaultCapPerDay: num(planning["default_cap_per_day"]),
    removalMode: (str(removal["mode"], "required") || "required") as RemovalMode,
    removalInstruction: str(removal["instruction"]),
    removalCutoffTime: str(removal["cutoff_time"]),
    removalProofs: proofs,
    removalQuestions: questions,
    individualVideoRequired: individual["video_required"] !== false,
    lumpSumVideoMin: num(lumpSum["video_min"]),
    lumpSumVideoMax: num(lumpSum["video_max"]),
    weightsFromMode,
    weightsFromDate: str(weights["default_from_date"], SEEDED_WEIGHTS_FROM_DATE) || SEEDED_WEIGHTS_FROM_DATE,
    weightsFromDays: num(weights["default_from_days"]) || "60",
    weightsFromWeeks: num(weights["default_from_weeks"]) || "6",
    weightsEarliestDate: str(weights["earliest_date"], SEEDED_WEIGHTS_EARLIEST_DATE) || SEEDED_WEIGHTS_EARLIEST_DATE,
  };
}

function emitQuestion(q: WeighingQuestionRow): Record<string, unknown> {
  const out: Record<string, unknown> = { id: q.key, kind: q.kind, title: q.title };
  if (q.hint.trim()) out.hint = q.hint;
  out.required = q.required;
  if (q.kind === "choice" || q.kind === "multi") {
    out.options = q.options.map((o) => ({ value: o.value.trim(), label: o.label.trim() })).filter((o) => o.value && o.label);
    if (q.allowOther) out.allow_other = true;
  }
  if (q.kind === "number") {
    if (q.min.trim() !== "" && Number.isFinite(Number(q.min))) out.min = Number(q.min);
    if (q.max.trim() !== "" && Number.isFinite(Number(q.max))) out.max = Number(q.max);
    if (q.unit.trim()) out.unit = q.unit.trim();
  }
  if (q.onlyIfQuestion) out.only_if = { question_id: q.onlyIfQuestion, value: q.onlyIfValue };
  return out;
}

export function emitWeighing(rows: WeighingRows): Record<string, unknown> {
  const removal: Record<string, unknown> = { mode: rows.removalMode };
  if (rows.removalInstruction.trim()) removal.instruction = rows.removalInstruction;
  if (rows.removalCutoffTime.trim()) removal.cutoff_time = rows.removalCutoffTime.trim();
  removal.proofs = rows.removalProofs.map((p) => {
    const out: Record<string, unknown> = { key: p.key, title: p.title };
    if (p.hint.trim()) out.hint = p.hint;
    out.kind = p.kind;
    out.required = p.required;
    return out;
  });
  removal.questions = rows.removalQuestions.map(emitQuestion);
  return {
    schema_version: WEIGHING_SCHEMA_VERSION,
    planning: { modes: rows.modes, default_cap_per_day: Number(rows.defaultCapPerDay) },
    feed_water_removal: removal,
    capture: {
      individual: { video_required: rows.individualVideoRequired },
      lump_sum: { video_min: Number(rows.lumpSumVideoMin), video_max: Number(rows.lumpSumVideoMax) },
    },
    weights_pages:
      rows.weightsFromMode === "rolling_weeks"
        ? { default_from_mode: "rolling_weeks", default_from_weeks: Number(rows.weightsFromWeeks), earliest_date: rows.weightsEarliestDate.trim() }
        : rows.weightsFromMode === "rolling_days"
        ? { default_from_mode: "rolling_days", default_from_days: Number(rows.weightsFromDays), earliest_date: rows.weightsEarliestDate.trim() }
        : { default_from_mode: "fixed_date", default_from_date: rows.weightsFromDate.trim(), earliest_date: rows.weightsEarliestDate.trim() },
  };
}

// Client-side pre-checks mirroring the backend validator's cheapest rules; the backend stays the
// authority (its 400 names the path too).
export function weighingProblems(rows: WeighingRows): string[] {
  const problems: string[] = [];
  if (rows.modes.length === 0) problems.push("Offer at least one way of weighing");
  const cap = Number(rows.defaultCapPerDay);
  if (!rows.defaultCapPerDay.trim() || !Number.isInteger(cap) || cap < 1 || cap > 10000) problems.push("Default animals per day must be a whole number from 1 to 10000");
  if (!REMOVAL_MODES.includes(rows.removalMode)) problems.push("Say when feed & water removal applies");
  if (rows.removalCutoffTime.trim() && !/^([01]\d|2[0-3]):[0-5]\d$/.test(rows.removalCutoffTime.trim())) problems.push("The removal evening must be a time like 20:00");
  if (rows.removalMode !== "off") {
    if (rows.removalProofs.length === 0) problems.push("The removal card needs at least one capture");
    if (rows.removalProofs.length > MAX_REMOVAL_PROOF_SLOTS) problems.push(`At most ${MAX_REMOVAL_PROOF_SLOTS} captures per pen`);
    if (!rows.removalProofs.some((p) => p.required)) problems.push("At least one capture must be compulsory — a removal is proven by something the verifier can see");
  }
  const seenSlot = new Set<string>();
  rows.removalProofs.forEach((p, pi) => {
    const at = `Capture ${pi + 1}`;
    if (!p.key.trim()) problems.push(`${at}: needs a key`);
    if (seenSlot.has(p.key)) problems.push(`${at}: key "${p.key}" is used twice`);
    seenSlot.add(p.key);
    if (!p.title.trim()) problems.push(`${at}: needs a title`);
  });
  const seen = new Map<string, WeighingQuestionRow>();
  rows.removalQuestions.forEach((q, qi) => {
    const at = `Removal question ${qi + 1}`;
    if (!q.key.trim()) problems.push(`${at}: needs a key`);
    if (q.key.endsWith("_other")) problems.push(`${at}: a key cannot end in _other`);
    if (seen.has(q.key)) problems.push(`${at}: key "${q.key}" is used twice`);
    if (!q.title.trim()) problems.push(`${at}: needs the question text`);
    if ((q.kind === "choice" || q.kind === "multi") && q.options.filter((o) => o.value.trim() && o.label.trim()).length === 0) problems.push(`${at}: a pick-one / pick-many question needs at least one choice`);
    if (q.allowOther && q.kind !== "choice") problems.push(`${at}: free-text "other" is only supported for pick-one questions`);
    if (q.allowOther && !q.options.some((o) => o.value.trim() === "other")) problems.push(`${at}: the free-text "other" needs a choice whose value is "other"`);
    if (q.kind === "number") {
      if (q.min.trim() && !Number.isFinite(Number(q.min))) problems.push(`${at}: min must be a finite number`);
      if (q.max.trim() && !Number.isFinite(Number(q.max))) problems.push(`${at}: max must be a finite number`);
      if (q.min.trim() && q.max.trim() && Number(q.min) > Number(q.max)) problems.push(`${at}: min must not exceed max`);
    }
    if (q.onlyIfQuestion) {
      const dep = seen.get(q.onlyIfQuestion);
      if (!dep) problems.push(`${at}: "ask only when" must name an earlier question`);
      else if (dep.kind !== "choice") problems.push(`${at}: "ask only when" must name a pick-one question`);
      else if (!dep.options.some((o) => o.value === q.onlyIfValue)) problems.push(`${at}: "ask only when" needs one of that question's choices`);
    }
    seen.set(q.key, q);
  });
  if (!rows.individualVideoRequired) problems.push("The per-animal video cannot be switched off: it is what the verifier reviews");
  const min = Number(rows.lumpSumVideoMin);
  const max = Number(rows.lumpSumVideoMax);
  if (!Number.isInteger(min) || min < 1) problems.push("A whole pen needs at least 1 video");
  if (!Number.isInteger(max) || max < 1 || max > LUMP_SUM_VIDEO_CEILING) problems.push(`Whole-pen videos: at most ${LUMP_SUM_VIDEO_CEILING}`);
  if (Number.isInteger(min) && Number.isInteger(max) && min > max) problems.push("Whole-pen minimum videos must not exceed the maximum");
  if (!ISO_DAY.test(rows.weightsEarliestDate.trim())) problems.push("Weights pages: pick the earliest day the calendar offers");
  if (rows.weightsFromMode === "rolling_weeks") {
    const weeks = Number(rows.weightsFromWeeks);
    if (!Number.isInteger(weeks) || weeks < 1 || weeks > 520) problems.push("Weights pages: open from 1 to 520 weeks before today");
  } else if (rows.weightsFromMode === "rolling_days") {
    const days = Number(rows.weightsFromDays);
    if (!Number.isInteger(days) || days < 1 || days > 3650) problems.push("Weights pages: open on the last 1 to 3650 days");
  } else {
    if (!ISO_DAY.test(rows.weightsFromDate.trim())) problems.push("Weights pages: pick the day the pages open from");
    else if (ISO_DAY.test(rows.weightsEarliestDate.trim()) && rows.weightsFromDate.trim() < rows.weightsEarliestDate.trim()) {
      problems.push("Weights pages: the opening day cannot be before the earliest day the calendar offers");
    }
  }
  return problems;
}

export function blankProofSlot(): RemovalProofRow {
  return { id: newRowId("ws"), key: "", title: "", hint: "", kind: "video", required: true };
}
