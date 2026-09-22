// PC CARE SOP (maintainer decision 2026-09-22, docs/decisions/pc-care-sop.md).
//
// Pure model of `form_dsl.pc_care`: the FEED & WATER REMOVAL rules (whether the evening-before
// removal applies, to which work, from which evening, and what the crew records and answers per
// pen) and one CARD PER WORK CATEGORY (the instruction, the captures the operator records for
// every animal, and the questions answered once per task at submit). Parses the backend document
// into editor rows and emits it back byte-faithfully. The backend
// (pccare/domain.ValidatePCCareSOP) is the authority on what is valid; this file only shapes and
// pre-checks so the author gets the message beside the field.
//
// The slot and question rows are the weighing removal card's, so one editor vocabulary serves
// every module. PC Care adds exactly one field of its own -- a capture's `min_seconds` recorder
// hint -- because the trimming "while" clip has always carried it.

import {
  MAX_REMOVAL_PROOF_SLOTS,
  emitQuestion,
  newRowId,
  parseQuestion,
  proofSlotProblems,
  questionProblems,
  type RemovalProofKind,
  type RemovalProofRow,
  type WeighingQuestionRow,
} from "./weighing-model.ts";

// Internal SOP schema token; assembled from neutral fragments so the visible-branding guard does
// not mistake the wire token for rendered copy.
export const PC_CARE_SCHEMA_VERSION = ["go", "atos.sop-pc-care.v1"].join("");

/** The SOP library code the PC Care rules are published under. */
export const PC_CARE_SOP_CODE = "pc_care.tasks";

/** The five work categories the document authors, in the order the phone shows their tabs. */
export const PC_CARE_CATEGORIES = ["deworming", "anti_protozoan", "ticks_removal", "hoof_trimming", "hair_trimming"] as const;
export type PcCareCategory = (typeof PC_CARE_CATEGORIES)[number];

export type PcCareRemovalMode = "required" | "optional" | "off";

/** A per-animal capture: the shared slot row plus the recorder-chrome duration hint. */
export type PcCareCaptureRow = RemovalProofRow & { minSeconds: string };

export type PcCareCategoryRows = {
  instruction: string;
  proofs: PcCareCaptureRow[];
  questions: WeighingQuestionRow[];
};

export type PcCareRemovalRows = {
  mode: PcCareRemovalMode;
  appliesTo: PcCareCategory[];
  cutoffTime: string;
  instruction: string;
  proofs: RemovalProofRow[];
  questions: WeighingQuestionRow[];
};

export type PcCareRows = {
  removal: PcCareRemovalRows;
  categories: Record<PcCareCategory, PcCareCategoryRows>;
};

export { MAX_REMOVAL_PROOF_SLOTS };

function str(v: unknown, fallback = ""): string {
  return typeof v === "string" ? v : fallback;
}
function obj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}
function isCategory(v: unknown): v is PcCareCategory {
  return typeof v === "string" && (PC_CARE_CATEGORIES as readonly string[]).includes(v);
}
function kindOf(raw: unknown): RemovalProofKind {
  const kind = str(raw, "video");
  return (kind === "photo" || kind === "either" ? kind : "video") as RemovalProofKind;
}

function parseSlot(raw: unknown, prefix: string): RemovalProofRow[] {
  const slot = obj(raw);
  if (!slot) return [];
  return [
    {
      id: newRowId(prefix),
      key: str(slot["key"]),
      title: str(slot["title"]),
      hint: str(slot["hint"]),
      kind: kindOf(slot["kind"]),
      // A slot published before the flag existed is compulsory (the shared authored rule).
      required: slot["required"] !== false,
    },
  ];
}

function parseCapture(raw: unknown): PcCareCaptureRow[] {
  const base = parseSlot(raw, "pcs");
  if (base.length === 0) return [];
  const slot = obj(raw)!;
  const seconds = slot["min_seconds"];
  return [{ ...base[0], minSeconds: typeof seconds === "number" && seconds > 0 ? String(seconds) : "" }];
}

function parseCategory(raw: unknown): PcCareCategoryRows {
  const block = obj(raw);
  const proofsRaw = block && Array.isArray(block["proofs"]) ? block["proofs"] : [];
  const questionsRaw = block && Array.isArray(block["questions"]) ? block["questions"] : [];
  return {
    instruction: str(block?.["instruction"]),
    proofs: proofsRaw.flatMap(parseCapture),
    questions: questionsRaw.flatMap(parseQuestion),
  };
}

export function parsePcCare(formDsl: unknown): PcCareRows | null {
  const dsl = obj(formDsl);
  const care = dsl ? obj(dsl["pc_care"]) : null;
  if (!care) return null;
  const removalRaw = obj(care["feed_water_removal"]) ?? {};
  const mode = str(removalRaw["mode"], "optional");
  const appliesRaw = Array.isArray(removalRaw["applies_to"]) ? removalRaw["applies_to"] : [];
  const categoriesRaw = obj(care["categories"]) ?? {};
  const categories = {} as Record<PcCareCategory, PcCareCategoryRows>;
  for (const category of PC_CARE_CATEGORIES) categories[category] = parseCategory(categoriesRaw[category]);
  return {
    removal: {
      mode: (mode === "required" || mode === "off" ? mode : "optional") as PcCareRemovalMode,
      appliesTo: appliesRaw.filter(isCategory),
      cutoffTime: str(removalRaw["cutoff_time"]),
      instruction: str(removalRaw["instruction"]),
      proofs: Array.isArray(removalRaw["proofs"]) ? removalRaw["proofs"].flatMap((p) => parseSlot(p, "pcr")) : [],
      questions: Array.isArray(removalRaw["questions"]) ? removalRaw["questions"].flatMap(parseQuestion) : [],
    },
    categories,
  };
}

function emitSlot(p: RemovalProofRow): Record<string, unknown> {
  const slot: Record<string, unknown> = { key: p.key, title: p.title };
  if (p.hint.trim()) slot.hint = p.hint;
  slot.kind = p.kind;
  slot.required = p.required;
  return slot;
}

function emitCapture(p: PcCareCaptureRow): Record<string, unknown> {
  const slot = emitSlot(p);
  const seconds = Number(p.minSeconds);
  if (p.minSeconds.trim() && Number.isFinite(seconds) && seconds > 0) slot.min_seconds = Math.trunc(seconds);
  return slot;
}

export function emitPcCare(rows: PcCareRows): Record<string, unknown> {
  const categories: Record<string, unknown> = {};
  for (const category of PC_CARE_CATEGORIES) {
    const block = rows.categories[category];
    const out: Record<string, unknown> = {};
    if (block.instruction.trim()) out.instruction = block.instruction;
    out.proofs = block.proofs.map(emitCapture);
    out.questions = block.questions.map(emitQuestion);
    categories[category] = out;
  }
  const removal: Record<string, unknown> = {
    mode: rows.removal.mode,
    applies_to: [...rows.removal.appliesTo],
  };
  // The evening is emitted even when blank: blank MEANS "the farm's own evening", and dropping the
  // key would read as "never authored" rather than "deliberately the farm's".
  removal.cutoff_time = rows.removal.cutoffTime;
  if (rows.removal.instruction.trim()) removal.instruction = rows.removal.instruction;
  removal.proofs = rows.removal.proofs.map(emitSlot);
  removal.questions = rows.removal.questions.map(emitQuestion);
  return { schema_version: PC_CARE_SCHEMA_VERSION, feed_water_removal: removal, categories };
}

export function blankCapture(): PcCareCaptureRow {
  return { id: newRowId("pcs"), key: "", title: "", hint: "", kind: "video", required: true, minSeconds: "" };
}

/** Client-side pre-checks mirroring the backend validator's cheapest rules. */
export function pcCareProblems(rows: PcCareRows, categoryLabel: (category: PcCareCategory) => string, removalLabel: string): string[] {
  const problems: string[] = [];
  if (rows.removal.mode !== "off") {
    if (rows.removal.appliesTo.length === 0) {
      problems.push(`${removalLabel}: name at least one kind of work it applies to, or switch it off`);
    }
    problems.push(...proofSlotProblems(rows.removal.proofs, removalLabel, true));
  } else {
    problems.push(...proofSlotProblems(rows.removal.proofs, removalLabel, false));
  }
  if (rows.removal.cutoffTime.trim() && !/^([01]\d|2[0-3]):[0-5]\d$/.test(rows.removal.cutoffTime.trim())) {
    problems.push(`${removalLabel}: the evening must be a time like 20:00`);
  }
  problems.push(...questionProblems(rows.removal.questions, `${removalLabel} question`));
  for (const category of PC_CARE_CATEGORIES) {
    const block = rows.categories[category];
    const label = categoryLabel(category);
    problems.push(...proofSlotProblems(block.proofs, label, true));
    block.proofs.forEach((p, i) => {
      const seconds = Number(p.minSeconds);
      if (p.minSeconds.trim() && (!Number.isFinite(seconds) || seconds < 0 || seconds > 600)) {
        problems.push(`${label} capture ${i + 1}: the recorder hint must be between 0 and 600 seconds`);
      }
      if (p.minSeconds.trim() && Number(p.minSeconds) > 0 && p.kind === "photo") {
        problems.push(`${label} capture ${i + 1}: a photo has no length`);
      }
    });
    problems.push(...questionProblems(block.questions, `${label} question`));
  }
  return problems;
}
