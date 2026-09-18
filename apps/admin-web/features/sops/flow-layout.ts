// Flow layout for a follow-up track (SOP studio phase 2, 2026-09-18). Pure: rows in, positioned
// nodes and edges out, so the chart can be unit-tested without a browser. The chart reads the
// track EXACTLY as the engine runs it: steps in order down a spine; a question whose answer
// gates later steps grows a DECISION with one column per condition (the branch steps chain
// under it) and an "otherwise" line straight on to the next unconditional step.
import type { AnswerOp, FollowUpStepRow } from "./followup-model";

export type FlowCondition = { whenStep: string; whenOp: AnswerOp; whenValues: string[] };

export type FlowNode = {
  id: string;
  kind: "start" | "step" | "question" | "decision" | "finish";
  x: number;
  y: number;
  w: number;
  h: number;
  /** The step this node renders (step / question), else undefined. */
  step?: FollowUpStepRow;
  /** Row index in the track (step / question). */
  index?: number;
};

export type FlowInsert = {
  /** Track index the new step is inserted at. */
  index: number;
  /** The branch condition the new step inherits (null = unconditional). */
  when: FlowCondition | null;
};

export type FlowEdge = {
  id: string;
  from: string;
  to: string;
  /** Branch label on a decision edge (the condition sentence); "" on a plain edge. */
  label: string;
  /** Where a step inserted on this line goes. */
  insert: FlowInsert;
};

export type FlowLayout = { nodes: FlowNode[]; edges: FlowEdge[]; width: number; height: number };

export const NODE_W = 230;
export const NODE_H = 78;
export const DECISION_H = 56;
const GAP_Y = 54;
const COL_X = 270;

function conditionKey(s: FollowUpStepRow): string {
  return `${s.whenStep}|${s.whenOp}|${s.whenValues.map((v) => v.trim().toLowerCase()).sort().join(",")}`;
}

function sameCondition(a: FlowCondition, b: FlowCondition): boolean {
  return a.whenStep === b.whenStep && a.whenOp === b.whenOp && a.whenValues.map((v) => v.trim().toLowerCase()).sort().join(",") === b.whenValues.map((v) => v.trim().toLowerCase()).sort().join(",");
}

/** Direct dependents of a question among the given steps, grouped by condition, in row order. */
export function branchesOf(question: FollowUpStepRow, scope: FollowUpStepRow[]): { condition: FlowCondition; steps: FollowUpStepRow[] }[] {
  const groups: { condition: FlowCondition; steps: FollowUpStepRow[] }[] = [];
  for (const s of scope) {
    if (s.whenStep !== question.key) continue;
    const cond: FlowCondition = { whenStep: s.whenStep, whenOp: s.whenOp, whenValues: s.whenValues };
    const group = groups.find((g) => sameCondition(g.condition, cond));
    if (group) group.steps.push(s);
    else groups.push({ condition: cond, steps: [s] });
  }
  return groups;
}

/** Transitive dependents (a step gated on a branch step belongs to that branch). */
function subtree(root: FollowUpStepRow[], scope: FollowUpStepRow[]): FollowUpStepRow[] {
  const keys = new Set(root.map((s) => s.key));
  const out: FollowUpStepRow[] = [];
  for (const s of scope) {
    if (keys.has(s.key) || (s.whenStep && keys.has(s.whenStep))) {
      keys.add(s.key);
      out.push(s);
    }
  }
  return out;
}

/** The condition a new step on the "otherwise" line gets: the branches' complement. */
export function otherwiseCondition(question: FollowUpStepRow, answerKind: string, branches: { condition: FlowCondition }[]): FlowCondition {
  const taken = branches.flatMap((b) => b.condition.whenValues);
  if (answerKind === "yes_no") {
    const value = taken.map((v) => v.toLowerCase()).includes("yes") ? "no" : "yes";
    return { whenStep: question.key, whenOp: "eq", whenValues: [value] };
  }
  if (answerKind === "select" || answerKind === "multiselect") {
    const rest = question.options.filter((o) => !taken.map((v) => v.toLowerCase()).includes(o.toLowerCase()));
    if (rest.length === 1) return { whenStep: question.key, whenOp: "eq", whenValues: rest };
    return { whenStep: question.key, whenOp: "not_in", whenValues: taken.length ? taken : question.options.slice(0, 1) };
  }
  return { whenStep: question.key, whenOp: "ne", whenValues: taken.slice(0, 1) };
}

/** The condition a NEW branch gets: the next value nobody branches on yet. */
export function nextBranchCondition(question: FollowUpStepRow, answerKind: string, branches: { condition: FlowCondition }[]): FlowCondition {
  const taken = new Set(branches.flatMap((b) => b.condition.whenValues.map((v) => v.toLowerCase())));
  if (answerKind === "yes_no") {
    return { whenStep: question.key, whenOp: "eq", whenValues: [taken.has("yes") ? "no" : "yes"] };
  }
  if (answerKind === "select" || answerKind === "multiselect") {
    const free = question.options.find((o) => !taken.has(o.toLowerCase())) ?? question.options[0] ?? "";
    return { whenStep: question.key, whenOp: "eq", whenValues: free ? [free] : [] };
  }
  if (answerKind === "number") return { whenStep: question.key, whenOp: "gt", whenValues: ["0"] };
  return { whenStep: question.key, whenOp: "eq", whenValues: [] };
}

/**
 * layoutTrack positions one track. `phrase` renders a branch label; `answerKind` tells which
 * steps are questions.
 */
export function layoutTrack(
  steps: FollowUpStepRow[],
  answerKind: (s: FollowUpStepRow) => string,
  phrase: (c: FlowCondition) => string,
  otherwiseLabel: string,
): FlowLayout {
  const nodes: FlowNode[] = [];
  const edges: FlowEdge[] = [];
  const indexOf = new Map(steps.map((s, i) => [s.id, i]));
  let maxX = 0;
  let maxY = 0;
  const place = (node: FlowNode) => {
    nodes.push(node);
    maxX = Math.max(maxX, node.x + node.w);
    maxY = Math.max(maxY, node.y + node.h);
  };
  const edge = (from: string, to: string, insert: FlowInsert, label = "") => edges.push({ id: `${from}->${to}`, from, to, label, insert });

  // chain lays out `scope` (row order) centred on x, starting at y; returns the ids whose
  // outgoing line continues the flow and the y below the last node.
  function chain(scope: FollowUpStepRow[], x: number, y: number, inherited: FlowCondition | null, from: { ids: string[]; labels: string[] }): { tails: string[]; bottom: number; lastInsert: FlowInsert } {
    let tails = from.ids;
    let labels = from.labels;
    let cursorY = y;
    let remaining = scope.slice();
    let lastInsert: FlowInsert = { index: scope.length ? indexOf.get(scope[0].id)! : steps.length, when: inherited };
    while (remaining.length > 0) {
      const s = remaining[0];
      remaining = remaining.slice(1);
      const idx = indexOf.get(s.id)!;
      const kind = answerKind(s);
      const isQuestion = !!kind && kind !== "none";
      const node: FlowNode = { id: s.id, kind: isQuestion ? "question" : "step", x: x - NODE_W / 2, y: cursorY, w: NODE_W, h: NODE_H, step: s, index: idx };
      place(node);
      tails.forEach((t, k) => edge(t, s.id, { index: idx, when: inherited }, labels[k] ?? ""));
      cursorY += NODE_H + GAP_Y;
      const branches = isQuestion ? branchesOf(s, remaining) : [];
      if (branches.length === 0) {
        tails = [s.id];
        labels = [""];
        lastInsert = { index: idx + 1, when: inherited };
        continue;
      }
      // A decision under the question, one column per branch, the otherwise line straight on.
      const decision: FlowNode = { id: `${s.id}:decision`, kind: "decision", x: x - NODE_W / 2, y: cursorY, w: NODE_W, h: DECISION_H, step: s, index: idx };
      place(decision);
      edge(s.id, decision.id, { index: idx + 1, when: inherited });
      cursorY += DECISION_H + GAP_Y;
      // Branch columns sit beside the spine -- left, right, further left, further right -- and
      // never ON it, so the otherwise line runs straight down the centre through no node.
      const branchTails: string[] = [];
      const branchLabels: string[] = [];
      let deepest = cursorY;
      branches.forEach((b, bi) => {
        const members = subtree(b.steps, remaining);
        remaining = remaining.filter((r) => !members.includes(r));
        const slot = Math.floor(bi / 2) + 1;
        const bx = x + (bi % 2 === 0 ? -slot : slot) * COL_X;
        const first = members[0];
        const result = chain(members, bx, cursorY, b.condition, { ids: [decision.id], labels: [phrase(b.condition)] });
        void first;
        branchTails.push(...result.tails);
        branchLabels.push(...result.tails.map(() => ""));
        deepest = Math.max(deepest, result.bottom);
      });
      // The otherwise line: from the decision to whatever comes next.
      const otherwise = otherwiseCondition(s, kind, branches);
      tails = [...branchTails, decision.id];
      labels = [...branchLabels, otherwiseLabel];
      cursorY = deepest;
      // A step inserted on the merge line after the branches is unconditional again.
      lastInsert = { index: remaining.length ? indexOf.get(remaining[0].id)! : Math.max(...scope.map((m) => indexOf.get(m.id)!)) + 1, when: inherited };
      // Insertion on the otherwise line itself gets the complement condition, exposed through
      // a dedicated edge: the decision's last tail carries it until the merge edge is drawn.
      otherwiseInsert.set(decision.id, { index: lastInsert.index, when: otherwise });
    }
    return { tails, bottom: cursorY, lastInsert };
  }
  const otherwiseInsert = new Map<string, FlowInsert>();
  const start: FlowNode = { id: "start", kind: "start", x: 0, y: 0, w: NODE_W, h: NODE_H };
  const centreX = NODE_W / 2 + COL_X * 2;
  start.x = centreX - NODE_W / 2;
  place(start);
  const result = chain(steps, centreX, NODE_H + GAP_Y, null, { ids: ["start"], labels: [""] });
  const finish: FlowNode = { id: "finish", kind: "finish", x: centreX - NODE_W / 2, y: result.bottom, w: NODE_W, h: NODE_H };
  place(finish);
  result.tails.forEach((t) => {
    const insert = otherwiseInsert.get(t) ?? result.lastInsert;
    edge(t, "finish", insert, otherwiseInsert.has(t) ? otherwiseLabel : "");
  });
  // Merge edges out of a decision (otherwise lines to a later spine step) carry the
  // complement condition so a step inserted there lands on that path.
  for (const e of edges) {
    if (otherwiseInsert.has(e.from) && e.to !== "finish" && (e.label === "" || e.label === otherwiseLabel)) {
      const oi = otherwiseInsert.get(e.from)!;
      e.insert = { index: e.insert.index, when: oi.when };
      if (!e.label) e.label = otherwiseLabel;
    }
  }
  // Nothing may sit left of 0.
  const minX = Math.min(...nodes.map((n) => n.x));
  if (minX < 0) for (const n of nodes) n.x -= minX;
  return { nodes, edges, width: Math.max(...nodes.map((n) => n.x + n.w)) + 24, height: maxY + 24 };
}
