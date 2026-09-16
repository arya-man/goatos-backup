// SHIFTING SOP: the editor model round-trips the seeded document byte-faithfully and pre-checks the
// cheapest rules the backend enforces.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { emitShifting, parseShifting, shiftingProblems } from "./shifting-model.ts";

const seed = JSON.parse(readFileSync(new URL("../../../../backend/internal/counts/domain/sopseed/shifting.json", import.meta.url), "utf8"));
const label = (s) => s;

test("the seeded document round-trips unchanged", () => {
  const rows = parseShifting({ shifting: seed });
  assert.ok(rows);
  assert.deepEqual(emitShifting(rows), seed);
  assert.deepEqual(shiftingProblems(rows, label), []);
});

test("a document without the section is not a shifting SOP", () => {
  assert.equal(parseShifting({ follow_up: {} }), null);
  assert.equal(parseShifting(null), null);
});

test("raise may be questions-only; a compulsory capture is required on completion and high priority", () => {
  const rows = parseShifting({ shifting: seed });
  rows.raise.questions.push({ id: "q", key: "why", kind: "text", title: "Why move", hint: "", required: true, options: [], allowOther: false, min: "", max: "", unit: "", onlyIfQuestion: "", onlyIfValue: "" });
  assert.deepEqual(shiftingProblems(rows, label), []);
  assert.equal(emitShifting(rows).raise.questions[0].id, "why");
  rows.completion.proofs[0].required = false;
  assert.ok(shiftingProblems(rows, label).some((m) => /completion/.test(m) && /compulsory/.test(m)));
  rows.completion.proofs[0].required = true;
  rows.high_priority.proofs = [];
  assert.ok(shiftingProblems(rows, label).some((m) => /high_priority/.test(m)));
});

test("a capture key or question key repeated across cards is named", () => {
  const rows = parseShifting({ shifting: seed });
  rows.raise.proofs.push({ id: "x", key: "shifting_shifting_video", title: "Dup", hint: "", kind: "photo", required: false });
  const problems = shiftingProblems(rows, label);
  assert.ok(problems.some((m) => /^completion: .*already used on raise/.test(m)), problems.join("\n"));
});

test("an either slot beside the feed clips and a high-priority question are emitted in order", () => {
  const rows = parseShifting({ shifting: seed });
  rows.high_priority.proofs = [{ id: "y", key: "feed_clip", title: "Feed clip", hint: "", kind: "either", required: true }];
  rows.high_priority.questions.push({ id: "q2", key: "ate", kind: "choice", title: "Did they eat?", hint: "", required: true, options: [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }], allowOther: false, min: "", max: "", unit: "", onlyIfQuestion: "", onlyIfValue: "" });
  assert.deepEqual(shiftingProblems(rows, label), []);
  const doc = emitShifting(rows);
  assert.deepEqual(doc.high_priority.proofs.map((p) => [p.key, p.kind, p.required]), [["feed_clip", "either", true]]);
  assert.equal(doc.high_priority.questions[0].id, "ate");
  assert.equal(doc.completion.proofs[0].key, "shifting_shifting_video");
});
