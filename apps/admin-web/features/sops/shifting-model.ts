// SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md).
//
// Pure model of `form_dsl.shifting`: the three CARDS the shifting SOP authors on /counts/sops --
// the RAISE extras (questions and optional captures on the raise form, seen by the park head before
// approval), the COMPLETION card (what every completion captures and answers) and the HIGH-PRIORITY
// card (added to a high movement's completion; seeded: the two feed clips). Parses the backend
// document into editor rows and emits it back byte-faithfully. The backend
// (counts/domain.ValidateShiftingSOP) is the authority on what is valid; this file only shapes and
// pre-checks so the author gets the message beside the field. The slot and question rows are the
// weighing removal card's, so one editor vocabulary serves every module.

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

export const SHIFTING_SCHEMA_VERSION = "goatos.sop-shifting.v1";
export const SHIFTING_SOP_CODE = "shifting";

export type ShiftingSection = "raise" | "completion" | "high_priority";
export const SHIFTING_SECTIONS: ShiftingSection[] = ["raise", "completion", "high_priority"];

export type ShiftingSectionRows = {
  instruction: string;
  proofs: RemovalProofRow[];
  questions: WeighingQuestionRow[];
};

export type ShiftingRows = Record<ShiftingSection, ShiftingSectionRows>;

export { MAX_REMOVAL_PROOF_SLOTS };

function str(v: unknown, fallback = ""): string {
  return typeof v === "string" ? v : fallback;
}
function obj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

function parseSection(raw: unknown): ShiftingSectionRows {
  const block = obj(raw);
  if (!block) return { instruction: "", proofs: [], questions: [] };
  const proofsRaw = Array.isArray(block["proofs"]) ? block["proofs"] : [];
  const proofs: RemovalProofRow[] = proofsRaw.flatMap((p) => {
    const slot = obj(p);
    if (!slot) return [];
    const kind = str(slot["kind"], "video");
    return [
      {
        id: newRowId("ss"),
        key: str(slot["key"]),
        title: str(slot["title"]),
        hint: str(slot["hint"]),
        kind: (kind === "photo" || kind === "either" ? kind : "video") as RemovalProofKind,
        // A slot published before the flag existed is compulsory.
        required: slot["required"] !== false,
      },
    ];
  });
  const questions = Array.isArray(block["questions"]) ? block["questions"].flatMap(parseQuestion) : [];
  return { instruction: str(block["instruction"]), proofs, questions };
}

// parseShifting reads a `shifting` section; null when the document carries none (a version
// published before the section existed, or another SOP).
export function parseShifting(formDsl: unknown): ShiftingRows | null {
  const dsl = obj(formDsl);
  const shifting = dsl ? obj(dsl["shifting"]) : null;
  if (!shifting) return null;
  return {
    raise: parseSection(shifting["raise"]),
    completion: parseSection(shifting["completion"]),
    high_priority: parseSection(shifting["high_priority"]),
  };
}

function emitSection(rows: ShiftingSectionRows): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  // The seed carries an empty instruction on every card; emitting the key keeps the round trip
  // byte-faithful and a blank instruction stays blank on the phone.
  out.instruction = rows.instruction;
  out.proofs = rows.proofs.map((p) => {
    const slot: Record<string, unknown> = { key: p.key, title: p.title };
    if (p.hint.trim()) slot.hint = p.hint;
    slot.kind = p.kind;
    slot.required = p.required;
    return slot;
  });
  out.questions = rows.questions.map(emitQuestion);
  return out;
}

export function emitShifting(rows: ShiftingRows): Record<string, unknown> {
  return {
    schema_version: SHIFTING_SCHEMA_VERSION,
    raise: emitSection(rows.raise),
    completion: emitSection(rows.completion),
    high_priority: emitSection(rows.high_priority),
  };
}

// Client-side pre-checks mirroring the backend validator's cheapest rules: completion and
// high-priority each need a compulsory capture, raise may be empty or questions-only, and slot keys
// / question keys are unique ACROSS the three cards (one capture register, one answer map).
export function shiftingProblems(rows: ShiftingRows, sectionLabel: (section: ShiftingSection) => string): string[] {
  const problems: string[] = [];
  problems.push(...proofSlotProblems(rows.raise.proofs, sectionLabel("raise"), false));
  problems.push(...proofSlotProblems(rows.completion.proofs, sectionLabel("completion"), true));
  problems.push(...proofSlotProblems(rows.high_priority.proofs, sectionLabel("high_priority"), true));
  for (const section of SHIFTING_SECTIONS) {
    problems.push(...questionProblems(rows[section].questions, `${sectionLabel(section)} question`));
  }
  const seenKeys = new Map<string, ShiftingSection>();
  const seenQuestions = new Map<string, ShiftingSection>();
  for (const section of SHIFTING_SECTIONS) {
    for (const p of rows[section].proofs) {
      const key = p.key.trim();
      if (!key) continue;
      const other = seenKeys.get(key);
      if (other && other !== section) problems.push(`${sectionLabel(section)}: capture key "${key}" is already used on ${sectionLabel(other)}`);
      else seenKeys.set(key, section);
    }
    for (const q of rows[section].questions) {
      const key = q.key.trim();
      if (!key) continue;
      const other = seenQuestions.get(key);
      if (other && other !== section) problems.push(`${sectionLabel(section)}: question key "${key}" is already used on ${sectionLabel(other)}`);
      else seenQuestions.set(key, section);
    }
  }
  return problems;
}
