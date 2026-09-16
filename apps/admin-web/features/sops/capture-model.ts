// HERD OPERATIONS CAPTURE CARD (maintainer decision 4, 2026-09-16).
//
// Pure model of `form_dsl.capture_card` on counts.birth / counts.death: the EXTRAS the Add birth /
// Add death form asks beside its fixed fields -- capture slots (a live-camera video, a photo or
// either; compulsory or optional) and questions -- authored on /counts/sops. An absent section is
// the EMPTY card (the plain form), and a card with questions and no captures is valid. Parses the
// backend document into editor rows and emits it back byte-faithfully; the backend
// (counts/domain.ValidateCaptureCard, run by countssop on save) is the authority. The slot and
// question rows are the weighing/feed card's, so one editor vocabulary serves every module.

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

export const CAPTURE_SCHEMA_VERSION = "goatos.sop-capture.v1";

// The SOP codes whose versions carry a capture card.
export const CAPTURE_CARD_CODES = ["counts.birth", "counts.death"];

export type CaptureRows = {
  instruction: string;
  proofs: RemovalProofRow[];
  questions: WeighingQuestionRow[];
};

export { MAX_REMOVAL_PROOF_SLOTS };

function str(v: unknown, fallback = ""): string {
  return typeof v === "string" ? v : fallback;
}
function obj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

export function isCaptureCardCode(sopCode: string): boolean {
  return CAPTURE_CARD_CODES.includes(sopCode);
}

// hasCaptureCard reports a version whose document already carries the section.
export function hasCaptureCard(formDsl: unknown): boolean {
  const dsl = obj(formDsl);
  return Boolean(dsl && obj(dsl["capture_card"]));
}

// parseCaptureCard returns the editor rows for a counts.birth / counts.death version (the empty
// card when the section is absent), or null for any other code.
export function parseCaptureCard(sopCode: string, formDsl: unknown): CaptureRows | null {
  if (!isCaptureCardCode(sopCode)) return null;
  const dsl = obj(formDsl);
  const card = dsl ? obj(dsl["capture_card"]) : null;
  if (!card) return { instruction: "", proofs: [], questions: [] };
  const proofsRaw = Array.isArray(card["proofs"]) ? card["proofs"] : [];
  const proofs: RemovalProofRow[] = proofsRaw.flatMap((p) => {
    const slot = obj(p);
    if (!slot) return [];
    const kind = str(slot["kind"], "video");
    return [
      {
        id: newRowId("cs"),
        key: str(slot["key"]),
        title: str(slot["title"]),
        hint: str(slot["hint"]),
        kind: (kind === "photo" || kind === "either" ? kind : "video") as RemovalProofKind,
        // A slot published before the flag existed is compulsory.
        required: slot["required"] !== false,
      },
    ];
  });
  const questions = Array.isArray(card["questions"]) ? card["questions"].flatMap(parseQuestion) : [];
  return { instruction: str(card["instruction"]), proofs, questions };
}

export function emitCaptureCard(rows: CaptureRows): Record<string, unknown> {
  const out: Record<string, unknown> = { schema_version: CAPTURE_SCHEMA_VERSION };
  if (rows.instruction.trim()) out.instruction = rows.instruction;
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

// captureProblems mirrors the backend's cheapest rules. Captures are OPTIONAL on a capture card
// (a questions-only form is allowed), so no compulsory-slot rule here.
export function captureProblems(rows: CaptureRows, proofsLabel: string, questionLabel: string): string[] {
  return [...proofSlotProblems(rows.proofs, proofsLabel, false), ...questionProblems(rows.questions, questionLabel)];
}
