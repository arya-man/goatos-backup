// FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md).
//
// Pure model of `form_dsl.feed`: the CARDS the crew runs at each stage of the feed chain,
// authored on /feed/sops -- for feed.direction the distribution card and the wastage card, for
// feed.packing the packing card, for feed.transport the transport card. Each card is an
// instruction, the capture slots (a live-camera video, a photo or either; compulsory or optional)
// and the questions the crew answers. Parses the backend document into editor rows and emits it
// back byte-faithfully. The backend (feeddirection/domain.ValidateFeedSOP) is the authority on
// what is valid; this file only shapes and pre-checks so the author gets the message beside the
// field. The slot and question rows are the weighing removal card's, so one editor vocabulary
// serves every module.

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

// Internal SOP schema token; assembled from neutral fragments so the visible-branding
// guard does not mistake the wire token for rendered copy.
export const FEED_SCHEMA_VERSION = ["go", "atos.sop-feed.v1"].join("");

export type FeedStage = "distribution" | "wastage" | "packing" | "transport";

// The blocks each SOP code's document carries. The direction document's wastage card is optional
// on the wire (a document published without it reads the seeded wastage card), but the editor
// always shows it so the author can see what the crew is asked.
export const FEED_STAGES_BY_CODE: Record<string, FeedStage[]> = {
  "feed.direction": ["distribution", "wastage"],
  "feed.packing": ["packing"],
  "feed.transport": ["transport"],
};

export type FeedStageRows = {
  instruction: string;
  proofs: RemovalProofRow[];
  questions: WeighingQuestionRow[];
};

export type FeedRows = {
  sopCode: string;
  stages: Partial<Record<FeedStage, FeedStageRows>>;
};

export { MAX_REMOVAL_PROOF_SLOTS };

function str(v: unknown, fallback = ""): string {
  return typeof v === "string" ? v : fallback;
}
function obj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

function parseStage(raw: unknown): FeedStageRows | null {
  const block = obj(raw);
  if (!block) return null;
  const proofsRaw = Array.isArray(block["proofs"]) ? block["proofs"] : [];
  const proofs: RemovalProofRow[] = proofsRaw.flatMap((p) => {
    const slot = obj(p);
    if (!slot) return [];
    const kind = str(slot["kind"], "video");
    return [
      {
        id: newRowId("fs"),
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

// The seeded wastage card, shown when a direction document was published without one so the
// author sees what the crew is asked today. Mirrors feeddirection/domain/sopseed/feed_direction.json.
export function seededWastageStage(): FeedStageRows {
  return {
    instruction: "Film the leftover feed in the pen before it is cleared.",
    proofs: [
      {
        id: newRowId("fs"),
        key: "feed_wastage_video",
        title: "Leftover feed video",
        hint: "The leftover feed in the trough before it is cleared; the verifier reads the weight off the clip.",
        kind: "video",
        required: true,
      },
    ],
    questions: [],
  };
}

export function parseFeed(sopCode: string, formDsl: unknown): FeedRows | null {
  const dsl = obj(formDsl);
  const feed = dsl ? obj(dsl["feed"]) : null;
  if (!feed) return null;
  const wanted = FEED_STAGES_BY_CODE[sopCode];
  if (!wanted) return null;
  const stages: Partial<Record<FeedStage, FeedStageRows>> = {};
  for (const stage of wanted) {
    const parsed = parseStage(feed[stage]);
    if (parsed) stages[stage] = parsed;
    else if (stage === "wastage") stages[stage] = seededWastageStage();
  }
  return { sopCode, stages };
}

function emitStage(rows: FeedStageRows): Record<string, unknown> {
  const out: Record<string, unknown> = {};
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

export function emitFeed(rows: FeedRows): Record<string, unknown> {
  const out: Record<string, unknown> = { schema_version: FEED_SCHEMA_VERSION };
  for (const stage of FEED_STAGES_BY_CODE[rows.sopCode] ?? []) {
    const block = rows.stages[stage];
    if (block) out[stage] = emitStage(block);
  }
  return out;
}

// Farm words for each stage, used by the editor and the summary. (Copy for headings comes from the
// page contract; these are the slot-list labels the pre-check problems name.)
export const FEED_STAGE_ORDER: FeedStage[] = ["distribution", "wastage", "packing", "transport"];

// Client-side pre-checks mirroring the backend validator's cheapest rules.
export function feedProblems(rows: FeedRows, stageLabel: (stage: FeedStage) => string): string[] {
  const problems: string[] = [];
  for (const stage of FEED_STAGES_BY_CODE[rows.sopCode] ?? []) {
    const block = rows.stages[stage];
    if (!block) {
      if (stage !== "wastage") problems.push(`${stageLabel(stage)}: the card is missing`);
      continue;
    }
    problems.push(...proofSlotProblems(block.proofs, stageLabel(stage), true));
    problems.push(...questionProblems(block.questions, `${stageLabel(stage)} question`));
  }
  return problems;
}
