import assert from "node:assert/strict";
import { test } from "node:test";
import { NODE_H, OWNER_H, branchesOf, layoutTrack, nextBranchCondition, otherwiseCondition } from "./flow-layout.ts";

const step = (key, extra = {}) => ({ id: key, key, taskType: "do_and_confirm", title: key, options: [], whenStep: "", whenOp: "eq", whenValues: [], ...extra });
const kinds = { ready: "yes_no", weight: "number", kind: "select" };
const answerKind = (s) => kinds[s.key] ?? "none";
const phrase = (c) => `${c.whenStep} ${c.whenOp} ${c.whenValues.join(",")}`;

test("a straight track is a spine from start to finish with one insert point per line", () => {
  const l = layoutTrack([step("a"), step("b")], answerKind, phrase, "Otherwise");
  assert.deepEqual(l.nodes.map((n) => n.kind), ["start", "step", "step", "finish"]);
  assert.deepEqual(l.edges.map((e) => `${e.from}->${e.to}:${e.insert.index}`), ["start->a:0", "a->b:1", "b->finish:2"]);
  assert.ok(l.edges.every((e) => e.insert.when === null));
});

test("a question with branches grows a decision, one column per condition, and an otherwise line", () => {
  const steps = [
    step("ready", { taskType: "record_yes_no" }),
    step("approve", { whenStep: "ready", whenValues: ["no"] }),
    step("note", { whenStep: "approve", whenValues: ["done"] }),
    step("finish_up"),
  ];
  const l = layoutTrack(steps, answerKind, phrase, "Otherwise");
  const kinds = Object.fromEntries(l.nodes.map((n) => [n.id, n.kind]));
  assert.equal(kinds["ready"], "question");
  assert.equal(kinds["ready:decision"], "decision");
  const branchEdge = l.edges.find((e) => e.from === "ready:decision" && e.to === "approve");
  assert.equal(branchEdge.label, "ready eq no");
  assert.deepEqual(branchEdge.insert, { index: 1, when: { whenStep: "ready", whenOp: "eq", whenValues: ["no"] } });
  // note is gated on approve, so it sits in approve's column below it.
  const approve = l.nodes.find((n) => n.id === "approve");
  const note = l.nodes.find((n) => n.id === "note");
  assert.equal(note.x, approve.x);
  assert.ok(note.y > approve.y);
  // The otherwise line goes from the decision to the merge step and inserts on the YES path.
  const otherwise = l.edges.find((e) => e.from === "ready:decision" && e.to === "finish_up");
  assert.equal(otherwise.label, "Otherwise");
  assert.deepEqual(otherwise.insert.when, { whenStep: "ready", whenOp: "eq", whenValues: ["yes"] });
  // The branch merges into the same step.
  assert.ok(l.edges.some((e) => e.from === "note" && e.to === "finish_up"));
  assert.ok(l.width > 0 && l.height > 0);
});

test("otherwise and next-branch conditions follow the answer kind", () => {
  const q = step("kind", { options: ["goat", "sheep", "cow"] });
  assert.deepEqual(otherwiseCondition(q, "select", [{ condition: { whenStep: "kind", whenOp: "eq", whenValues: ["goat"] } }]), { whenStep: "kind", whenOp: "not_in", whenValues: ["goat"] });
  assert.deepEqual(otherwiseCondition(q, "select", [{ condition: { whenStep: "kind", whenOp: "eq", whenValues: ["goat"] } }, { condition: { whenStep: "kind", whenOp: "eq", whenValues: ["sheep"] } }]), { whenStep: "kind", whenOp: "eq", whenValues: ["cow"] });
  assert.deepEqual(nextBranchCondition(q, "select", [{ condition: { whenStep: "kind", whenOp: "eq", whenValues: ["goat"] } }]), { whenStep: "kind", whenOp: "eq", whenValues: ["sheep"] });
  const yn = step("ready");
  assert.deepEqual(nextBranchCondition(yn, "yes_no", []), { whenStep: "ready", whenOp: "eq", whenValues: ["yes"] });
  assert.deepEqual(otherwiseCondition(yn, "yes_no", [{ condition: { whenStep: "ready", whenOp: "eq", whenValues: ["yes"] } }]), { whenStep: "ready", whenOp: "eq", whenValues: ["no"] });
  assert.equal(branchesOf(yn, [step("x", { whenStep: "ready", whenValues: ["yes"] }), step("y", { whenStep: "ready", whenValues: ["yes"] })]).length, 1);
});

// A question with no branch offers "Add branch" on itself and stands one row taller; once it has
// a branch the decision node carries the control and the question is an ordinary node again.
test("a question without a branch is branchable and taller; with a branch it is not", () => {
  const kind = (s) => (s.key === "q1" ? "yes_no" : "none");
  const plain = layoutTrack([step("q1"), step("a1")], kind, phrase);
  const qn = plain.nodes.find((n) => n.id === "q1");
  assert.equal(qn.branchable, true);
  assert.ok(qn.h > NODE_H);
  const branched = layoutTrack([step("q1"), step("a1", { whenStep: "q1", whenOp: "eq", whenValues: ["yes"] })], kind, phrase);
  const qb = branched.nodes.find((n) => n.id === "q1");
  assert.equal(qb.branchable, false);
  assert.equal(qb.h, NODE_H);
  assert.ok(branched.nodes.some((n) => n.kind === "decision"));
});

// SALES SOP (2026-09-19): a step that names who does it stands one row taller so its "Done by"
// chip never clips the title; an unowned step keeps the plain height.
test("an owned step stands one row taller than an unowned one", () => {
  const l = layoutTrack([step("a", { owner: "park_head" }), step("b")], answerKind, phrase, "Otherwise");
  const h = Object.fromEntries(l.nodes.map((n) => [n.id, n.h]));
  assert.equal(h["a"], NODE_H + OWNER_H);
  assert.equal(h["b"], NODE_H);
});
