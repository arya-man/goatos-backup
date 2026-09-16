// WEIGHING SOP (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md).
//
// Pure model of `form_dsl.weighing`: the rules a weighing task is planned on and runs under,
// authored on /weighing/sops -- the capture modes the planner may pick, the default cap per day,
// whether the evening-before feed & water removal is required / optional / off, the removal
// card's instruction, the two proof slots' wording, the questions the removal operator answers
// per pen, and the two weigh capture sections (THE WEIGH CAPTURES ARE AUTHORED, maintainer
// decision 2026-09-16): PER ANIMAL (slots + questions beside the RFID scan and the weight) and
// WHOLE PEN (counted slots + questions beside the total weight), authored independently and never
// merged. Parses the backend document into editor rows and emits
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

// A whole-pen capture slot carries a COUNT: how many the operator must record at least and may
// record at most (weighing/domain.CountedProofSlot).
export type CountedProofRow = { id: string; key: string; title: string; hint: string; kind: RemovalProofKind; min: string; max: string };

// The capture ceilings (weighing/domain: MaxIndividualProofSlots, MaxLumpSumProofSlots,
// MaxLumpSumProofsTotal, maxCaptureQuestions); the backend refuses more.
export const MAX_INDIVIDUAL_PROOF_SLOTS = 4;
export const MAX_LUMP_SUM_PROOF_SLOTS = 4;
export const MAX_LUMP_SUM_PROOFS_TOTAL = 10;
export const MAX_CAPTURE_QUESTIONS = 20;
const SLOT_KEY = /^[a-z][a-z0-9_]{0,47}$/;

// CAPTURE_DEFAULTS_COPY_KEY names the page-contract copy entry carrying the seeded slot document.
// Its value in adminui/app.weighingSOPEditorCopy is a Go constant (a JSON string), not a string
// literal, so the copy-keys guard's literal scan cannot see it declared; the screens read it
// through this constant and weighing-model.test.mjs pins the backend declaration instead.
export const CAPTURE_DEFAULTS_COPY_KEY = "wsop.capture.defaults";

// CaptureDefaults is the seeded slot document the backend serves as the `wsop.capture.defaults`
// copy string: what a document with no explicit slot list means. The web model never imports the
// backend's JSON; it is parsed from the page contract (parseCaptureDefaults) and, when that copy
// is absent, falls back to the minimal built-in below.
export type CaptureDefaultSlot = { key: string; title: string; hint: string; kind: RemovalProofKind; required: boolean };
export type CaptureDefaultCountedSlot = { key: string; title: string; hint: string; kind: RemovalProofKind; min: number; max: number };
export type CaptureDefaults = {
  individual: { proofs: CaptureDefaultSlot[]; questions: unknown[] };
  lump_sum: { proofs: CaptureDefaultCountedSlot[]; questions: unknown[] };
};
export type CaptureBaseline = { individualProofs: unknown[]; individualQuestions: unknown[]; lumpSumProofs: unknown[]; lumpSumQuestions: unknown[] };
export const BUILT_IN_CAPTURE_DEFAULTS: CaptureDefaults = {
  individual: { proofs: [{ key: "animal_video", title: "Weighing video", hint: "", kind: "video", required: true }], questions: [] },
  lump_sum: { proofs: [{ key: "pen_video", title: "Weighing video", hint: "", kind: "video", min: 1, max: 5 }], questions: [] },
};

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
  /** PER ANIMAL: captures and questions beside the RFID scan and the weight. */
  individualProofs: RemovalProofRow[];
  individualQuestions: WeighingQuestionRow[];
  /** True when the parsed document carried its own per-animal slot list (else derived from the defaults). */
  individualProofsExplicit: boolean;
  /** WHOLE PEN: counted captures and questions beside the total weight. */
  lumpSumProofs: CountedProofRow[];
  lumpSumQuestions: WeighingQuestionRow[];
  /** True when the parsed document carried its own whole-pen slot list (else derived from the defaults). */
  lumpSumProofsExplicit: boolean;
  /** The legacy whole-pen video window; the rule itself only while the whole-pen slots are derived, a mirror once they are emitted. */
  lumpSumVideoMin: string;
  lumpSumVideoMax: string;
  /** The two sections as they stood when parsed (emitted shape), so emit can tell "untouched" from "authored". */
  captureBaseline: CaptureBaseline;
  /** Legacy SOP metadata retained for document compatibility; never exposed as calendar controls. */
  weightsFromMode: WeightsFromMode;
  weightsFromDate: string;
  weightsFromDays: string;
  weightsEarliestDate: string;
};

export type WeightsFromMode = "fixed_date" | "rolling_days";

// The seeded window, which is what the pages hardcoded before the block existed.
export const SEEDED_WEIGHTS_FROM_DATE = "2026-08-03";
export const SEEDED_WEIGHTS_EARLIEST_DATE = "2026-08-01";
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

/**
 * keyForTitle is the key an authored capture / question carries after its title changes. A key the
 * LOADED version already has is never rewritten: it is what the phones stamp on uploads and what
 * answers are stored under. A key the author has not saved yet FOLLOWS the whole title -- the old
 * `key || slugKey(title)` froze it at the first keystroke, so a capture typed as "Trough photo"
 * shipped with the key "t" (E2E 2026-09-17). `siblings` may include the current key; it is ignored
 * when choosing the new one so a key never collides with itself.
 */
export function keyForTitle(title: string, currentKey: string, savedKeys: Set<string>, siblings: Set<string>, fallback = "question"): string {
  if (currentKey && savedKeys.has(currentKey)) return currentKey;
  const taken = new Set(siblings);
  taken.delete(currentKey);
  return slugKey(title, taken, fallback);
}

export function parseQuestion(rq: unknown): WeighingQuestionRow[] {
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

function kindOf(v: unknown): RemovalProofKind {
  const kind = str(v, "video");
  return kind === "photo" || kind === "either" ? kind : "video";
}

function parseSlot(raw: unknown): RemovalProofRow[] {
  const p = obj(raw);
  if (!p) return [];
  return [{
    id: newRowId("ws"),
    key: str(p["key"]),
    title: str(p["title"]),
    hint: str(p["hint"]),
    kind: kindOf(p["kind"]),
    // A slot published before the flag existed is compulsory (the shape those documents meant).
    required: p["required"] !== false,
  }];
}

function parseCountedSlot(raw: unknown): CountedProofRow[] {
  const p = obj(raw);
  if (!p) return [];
  return [{ id: newRowId("wc"), key: str(p["key"]), title: str(p["title"]), hint: str(p["hint"]), kind: kindOf(p["kind"]), min: num(p["min"]), max: num(p["max"]) }];
}

// parseCaptureDefaults reads the `wsop.capture.defaults` copy string; anything unreadable falls
// back to the built-in minimal defaults so the editor always has both sections.
export function parseCaptureDefaults(json: string | null | undefined): CaptureDefaults {
  if (!json) return BUILT_IN_CAPTURE_DEFAULTS;
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    return BUILT_IN_CAPTURE_DEFAULTS;
  }
  const d = obj(parsed);
  const ind = d ? obj(d["individual"]) : null;
  const lump = d ? obj(d["lump_sum"]) : null;
  const indProofs: CaptureDefaultSlot[] = ind && Array.isArray(ind["proofs"]) ? ind["proofs"].flatMap(parseSlot).map((p) => ({ key: p.key, title: p.title, hint: p.hint, kind: p.kind, required: p.required })) : [];
  const lumpProofs: CaptureDefaultCountedSlot[] = lump && Array.isArray(lump["proofs"]) ? lump["proofs"].flatMap(parseCountedSlot).map((p) => ({ key: p.key, title: p.title, hint: p.hint, kind: p.kind, min: Number(p.min || "0"), max: Number(p.max || "0") })) : [];
  if (indProofs.length === 0 || lumpProofs.length === 0) return BUILT_IN_CAPTURE_DEFAULTS;
  return {
    individual: { proofs: indProofs, questions: ind && Array.isArray(ind["questions"]) ? ind["questions"] : [] },
    lump_sum: { proofs: lumpProofs, questions: lump && Array.isArray(lump["questions"]) ? lump["questions"] : [] },
  };
}

// derivedIndividualProofs / derivedLumpSumProofs are what an ABSENT slot list means, exactly as
// the backend reads it (Rules.IndividualProofs / Rules.LumpSumProofs): the seeded slots, the
// whole-pen one carrying the document's OWN video window.
export function derivedIndividualProofs(defaults: CaptureDefaults): RemovalProofRow[] {
  return defaults.individual.proofs.map((p) => ({ id: newRowId("ws"), ...p }));
}
export function derivedLumpSumProofs(defaults: CaptureDefaults, videoMin: string, videoMax: string): CountedProofRow[] {
  const out = defaults.lump_sum.proofs.map((p) => ({ id: newRowId("wc"), ...p, min: String(p.min), max: String(p.max) }));
  if (out.length > 0 && videoMin.trim() !== "" && videoMax.trim() !== "") {
    out[0] = { ...out[0], min: videoMin, max: videoMax };
  }
  return out;
}
function derivedQuestions(raw: unknown[]): WeighingQuestionRow[] {
  return raw.flatMap(parseQuestion);
}

// withCaptureDefaults re-derives the non-explicit sections from `defaults` (the editor parses
// the document before it holds the page contract, then applies the contract's defaults here).
export function withCaptureDefaults(rows: WeighingRows, defaults: CaptureDefaults): WeighingRows {
  const next: WeighingRows = {
    ...rows,
    individualProofs: rows.individualProofsExplicit ? rows.individualProofs : derivedIndividualProofs(defaults),
    lumpSumProofs: rows.lumpSumProofsExplicit ? rows.lumpSumProofs : derivedLumpSumProofs(defaults, rows.lumpSumVideoMin, rows.lumpSumVideoMax),
  };
  next.captureBaseline = captureBaselineOf(next);
  return next;
}

function captureBaselineOf(rows: Pick<WeighingRows, "individualProofs" | "individualQuestions" | "lumpSumProofs" | "lumpSumQuestions">): CaptureBaseline {
  return {
    individualProofs: rows.individualProofs.map(emitSlot),
    individualQuestions: rows.individualQuestions.map(emitQuestion),
    lumpSumProofs: rows.lumpSumProofs.map(emitCountedSlot),
    lumpSumQuestions: rows.lumpSumQuestions.map(emitQuestion),
  };
}

export function parseWeighing(formDsl: unknown, defaults: CaptureDefaults = BUILT_IN_CAPTURE_DEFAULTS): WeighingRows | null {
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
  const weightsFromMode: WeightsFromMode = weights["default_from_mode"] === "rolling_days" ? "rolling_days" : "fixed_date";
  const modes = Array.isArray(planning["modes"]) ? planning["modes"].filter((m): m is WeighingMode => m === "individual_animal" || m === "per_shed_partition") : [];
  const proofsRaw = Array.isArray(removal["proofs"]) ? removal["proofs"] : [];
  const proofs: RemovalProofRow[] = proofsRaw.flatMap(parseSlot);
  const questions = Array.isArray(removal["questions"]) ? removal["questions"].flatMap(parseQuestion) : [];
  const individualProofsExplicit = Array.isArray(individual["proofs"]);
  const lumpSumProofsExplicit = Array.isArray(lumpSum["proofs"]);
  const lumpSumVideoMin = num(lumpSum["video_min"]);
  const lumpSumVideoMax = num(lumpSum["video_max"]);
  const sections = {
    individualProofs: individualProofsExplicit ? (individual["proofs"] as unknown[]).flatMap(parseSlot) : derivedIndividualProofs(defaults),
    individualQuestions: Array.isArray(individual["questions"]) ? individual["questions"].flatMap(parseQuestion) : derivedQuestions(defaults.individual.questions),
    lumpSumProofs: lumpSumProofsExplicit ? (lumpSum["proofs"] as unknown[]).flatMap(parseCountedSlot) : derivedLumpSumProofs(defaults, lumpSumVideoMin, lumpSumVideoMax),
    lumpSumQuestions: Array.isArray(lumpSum["questions"]) ? lumpSum["questions"].flatMap(parseQuestion) : derivedQuestions(defaults.lump_sum.questions),
  };
  return {
    modes,
    defaultCapPerDay: num(planning["default_cap_per_day"]),
    removalMode: (str(removal["mode"], "required") || "required") as RemovalMode,
    removalInstruction: str(removal["instruction"]),
    removalCutoffTime: str(removal["cutoff_time"]),
    removalProofs: proofs,
    removalQuestions: questions,
    individualVideoRequired: individual["video_required"] !== false,
    ...sections,
    individualProofsExplicit,
    lumpSumProofsExplicit,
    lumpSumVideoMin,
    lumpSumVideoMax,
    captureBaseline: captureBaselineOf(sections),
    weightsFromMode,
    weightsFromDate: str(weights["default_from_date"], SEEDED_WEIGHTS_FROM_DATE) || SEEDED_WEIGHTS_FROM_DATE,
    weightsFromDays: num(weights["default_from_days"]) || "60",
    weightsEarliestDate: str(weights["earliest_date"], SEEDED_WEIGHTS_EARLIEST_DATE) || SEEDED_WEIGHTS_EARLIEST_DATE,
  };
}

export function emitQuestion(q: WeighingQuestionRow): Record<string, unknown> {
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

export function emitSlot(p: RemovalProofRow): Record<string, unknown> {
  const out: Record<string, unknown> = { key: p.key, title: p.title };
  if (p.hint.trim()) out.hint = p.hint;
  out.kind = p.kind;
  out.required = p.required;
  return out;
}

export function emitCountedSlot(p: CountedProofRow): Record<string, unknown> {
  const out: Record<string, unknown> = { key: p.key, title: p.title };
  if (p.hint.trim()) out.hint = p.hint;
  out.kind = p.kind;
  out.min = Number(p.min);
  out.max = Number(p.max);
  return out;
}

function sameJSON(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

// A section's slots and questions are written to the document only when the parsed document
// carried them or the author moved them away from what was derived at parse time; an untouched
// seed emits the seed.
export function individualSectionEmitted(rows: WeighingRows): boolean {
  if (rows.individualProofsExplicit) return true;
  const b = rows.captureBaseline;
  return !sameJSON(rows.individualProofs.map(emitSlot), b.individualProofs) || !sameJSON(rows.individualQuestions.map(emitQuestion), b.individualQuestions);
}
export function lumpSumSectionEmitted(rows: WeighingRows): boolean {
  if (rows.lumpSumProofsExplicit) return true;
  const b = rows.captureBaseline;
  return !sameJSON(rows.lumpSumProofs.map(emitCountedSlot), b.lumpSumProofs) || !sameJSON(rows.lumpSumQuestions.map(emitQuestion), b.lumpSumQuestions);
}

// legacyVideoWindow mirrors the whole-pen slots into the window older phones read: the sum of the
// video / either slots' counts, clamped to 1..LUMP_SUM_VIDEO_CEILING (weighing/domain.legacyVideoWindow).
export function legacyVideoWindow(slots: CountedProofRow[]): { min: number; max: number } {
  let lo = 0;
  let hi = 0;
  for (const s of slots) {
    if (s.kind === "video" || s.kind === "either") {
      lo += Number(s.min) || 0;
      hi += Number(s.max) || 0;
    }
  }
  if (lo < 1) lo = 1;
  if (hi > LUMP_SUM_VIDEO_CEILING) hi = LUMP_SUM_VIDEO_CEILING;
  if (hi < lo) hi = lo;
  return { min: lo, max: hi };
}

export function emitWeighing(rows: WeighingRows): Record<string, unknown> {
  const removal: Record<string, unknown> = { mode: rows.removalMode };
  if (rows.removalInstruction.trim()) removal.instruction = rows.removalInstruction;
  if (rows.removalCutoffTime.trim()) removal.cutoff_time = rows.removalCutoffTime.trim();
  removal.proofs = rows.removalProofs.map(emitSlot);
  removal.questions = rows.removalQuestions.map(emitQuestion);
  const individual: Record<string, unknown> = { video_required: rows.individualVideoRequired };
  if (individualSectionEmitted(rows)) {
    individual.proofs = rows.individualProofs.map(emitSlot);
    individual.questions = rows.individualQuestions.map(emitQuestion);
  }
  let lumpSum: Record<string, unknown>;
  if (lumpSumSectionEmitted(rows)) {
    const window = legacyVideoWindow(rows.lumpSumProofs);
    lumpSum = { video_min: window.min, video_max: window.max, proofs: rows.lumpSumProofs.map(emitCountedSlot), questions: rows.lumpSumQuestions.map(emitQuestion) };
  } else {
    lumpSum = { video_min: Number(rows.lumpSumVideoMin), video_max: Number(rows.lumpSumVideoMax) };
  }
  return {
    schema_version: WEIGHING_SCHEMA_VERSION,
    planning: { modes: rows.modes, default_cap_per_day: Number(rows.defaultCapPerDay) },
    feed_water_removal: removal,
    capture: { individual, lump_sum: lumpSum },
    weights_pages:
      rows.weightsFromMode === "rolling_days"
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
  problems.push(...questionProblems(rows.removalQuestions, "Removal question"));
  if (!rows.individualVideoRequired) problems.push("The per-animal video cannot be switched off: it is what the verifier reviews");
  // PER ANIMAL.
  problems.push(...proofSlotProblems(rows.individualProofs, "Per animal", true, MAX_INDIVIDUAL_PROOF_SLOTS));
  if (rows.individualQuestions.length > MAX_CAPTURE_QUESTIONS) problems.push(`Per-animal questions: at most ${MAX_CAPTURE_QUESTIONS}`);
  problems.push(...questionProblems(rows.individualQuestions, "Per-animal question"));
  // WHOLE PEN.
  problems.push(...countedSlotProblems(rows.lumpSumProofs, "Whole pen"));
  if (rows.lumpSumQuestions.length > MAX_CAPTURE_QUESTIONS) problems.push(`Whole-pen questions: at most ${MAX_CAPTURE_QUESTIONS}`);
  problems.push(...questionProblems(rows.lumpSumQuestions, "Whole-pen question"));
  // The legacy window is the rule only while the whole-pen slots are derived; once they are
  // emitted it is a computed mirror and is not judged (weighing/domain.validateCaptureSections).
  if (!lumpSumSectionEmitted(rows)) {
    const min = Number(rows.lumpSumVideoMin);
    const max = Number(rows.lumpSumVideoMax);
    if (!Number.isInteger(min) || min < 1) problems.push("A whole pen needs at least 1 video");
    if (!Number.isInteger(max) || max < 1 || max > LUMP_SUM_VIDEO_CEILING) problems.push(`Whole-pen videos: at most ${LUMP_SUM_VIDEO_CEILING}`);
    if (Number.isInteger(min) && Number.isInteger(max) && min > max) problems.push("Whole-pen minimum videos must not exceed the maximum");
  }
  if (!ISO_DAY.test(rows.weightsEarliestDate.trim())) problems.push("Weights pages: pick the earliest day the calendar offers");
  if (rows.weightsFromMode === "rolling_days") {
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

export function blankCountedSlot(): CountedProofRow {
  return { id: newRowId("wc"), key: "", title: "", hint: "", kind: "video", min: "1", max: "1" };
}

// questionProblems pre-checks a question list the way the backend validator does; shared by the
// weighing removal card and the feed cards (FEED SOP, 2026-09-16).
export function questionProblems(questions: WeighingQuestionRow[], label: string): string[] {
  const problems: string[] = [];
  const seen = new Map<string, WeighingQuestionRow>();
  questions.forEach((q, qi) => {
    const at = `${label} ${qi + 1}`;
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
  return problems;
}

// slotIdentityProblems pre-checks what every capture slot shares: a well-formed unique key and a
// title (weighing/domain.validateSlotIdentity).
function slotIdentityProblems(slots: { key: string; title: string }[], label: string): string[] {
  const problems: string[] = [];
  const seenSlot = new Set<string>();
  slots.forEach((p, pi) => {
    const at = `${label}, capture ${pi + 1}`;
    if (!p.key.trim()) problems.push(`${at}: needs a key`);
    else if (!SLOT_KEY.test(p.key)) problems.push(`${at}: key must be lowercase letters, digits and underscores, starting with a letter`);
    if (seenSlot.has(p.key)) problems.push(`${at}: key "${p.key}" is used twice`);
    seenSlot.add(p.key);
    if (!p.title.trim()) problems.push(`${at}: needs a title`);
  });
  return problems;
}

// proofSlotProblems pre-checks a capture slot list: keys present and unique, titles present, at
// least one compulsory slot when requireOne, at most `maxSlots` (the removal card's ceiling by default).
export function proofSlotProblems(slots: RemovalProofRow[], label: string, requireOne: boolean, maxSlots = MAX_REMOVAL_PROOF_SLOTS): string[] {
  const problems: string[] = [];
  if (requireOne && slots.length === 0) problems.push(`${label}: needs at least one capture`);
  if (slots.length > maxSlots) problems.push(`${label}: at most ${maxSlots} captures`);
  if (requireOne && slots.length > 0 && !slots.some((p) => p.required)) problems.push(`${label}: at least one capture must be compulsory — the work is proven by something the verifier can see`);
  problems.push(...slotIdentityProblems(slots, label));
  return problems;
}

// countedSlotProblems pre-checks the whole-pen slot list: identity, each count a whole number
// (at least 0.., at most 1..5, at least <= at most), at least one slot with a minimum of 1, and
// the row's total ceiling (weighing/domain.validateCaptureSections, whole-pen half).
export function countedSlotProblems(slots: CountedProofRow[], label: string): string[] {
  const problems: string[] = [];
  if (slots.length === 0) problems.push(`${label}: needs at least one capture`);
  if (slots.length > MAX_LUMP_SUM_PROOF_SLOTS) problems.push(`${label}: at most ${MAX_LUMP_SUM_PROOF_SLOTS} captures`);
  problems.push(...slotIdentityProblems(slots, label));
  let compulsory = 0;
  let total = 0;
  slots.forEach((p, pi) => {
    const at = `${label}, capture ${pi + 1}`;
    const min = Number(p.min);
    const max = Number(p.max);
    const minOK = p.min.trim() !== "" && Number.isInteger(min) && min >= 0;
    const maxOK = p.max.trim() !== "" && Number.isInteger(max) && max >= 1 && max <= LUMP_SUM_VIDEO_CEILING;
    if (!minOK) problems.push(`${at}: at least must be a whole number of 0 or more`);
    if (!maxOK) problems.push(`${at}: at most must be a whole number from 1 to ${LUMP_SUM_VIDEO_CEILING}`);
    if (minOK && maxOK && min > max) problems.push(`${at}: at least must not exceed at most`);
    if (minOK && min >= 1) compulsory += 1;
    if (maxOK) total += max;
  });
  if (slots.length > 0 && compulsory === 0) problems.push(`${label}: at least one capture must have a minimum of 1 — a whole-pen weigh is proven by something the verifier can see`);
  if (total > MAX_LUMP_SUM_PROOFS_TOTAL) problems.push(`${label}: at most ${MAX_LUMP_SUM_PROOFS_TOTAL} captures per pen in total`);
  return problems;
}
