import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { blankQuestion, emitWeighing, parseWeighing, weighingProblems } from "./weighing-model.ts";

// The seeded document is the same bytes migration 000314 adds to each tenant's published
// weighing.session version (pinned by the Go test TestMigrationEmbedsTheSeededWeighingSOP).
// Parsing it into editor rows and emitting it back must reproduce it EXACTLY, or opening the
// editor and pressing Publish with no edits would change what the planner and the phone run.
const seedPath = new URL("../../../../backend/internal/weighing/domain/sopseed/weighing_session.json", import.meta.url);

function canonical(v) {
  return JSON.stringify(v, (_k, val) => (val && typeof val === "object" && !Array.isArray(val) ? Object.fromEntries(Object.keys(val).sort().map((k) => [k, val[k]])) : val));
}

test("the seeded weighing rules round-trip through the editor model byte-faithfully", () => {
  const doc = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseWeighing({ weighing: doc });
  assert.ok(rows);
  assert.equal(rows.removalMode, "required");
  assert.deepEqual(rows.modes, ["individual_animal", "per_shed_partition"]);
  assert.deepEqual(rows.removalProofs.map((p) => p.key), ["feed_video", "water_video"]);
  assert.equal(rows.removalQuestions.length, 0);
  assert.equal(canonical(emitWeighing(rows)), canonical(doc));
  assert.deepEqual(weighingProblems(rows), []);
});

test("a form_dsl without the section is not a weighing SOP", () => {
  assert.equal(parseWeighing({ fields: [] }), null);
  assert.equal(parseWeighing(null), null);
});

test("pre-checks name the rule: no mode, per-animal video off, lump-sum window, question problems", () => {
  const rows = parseWeighing({ weighing: JSON.parse(readFileSync(seedPath, "utf8")) });
  rows.modes = [];
  rows.individualVideoRequired = false;
  rows.lumpSumVideoMin = "4";
  rows.lumpSumVideoMax = "9";
  rows.defaultCapPerDay = "0";
  rows.removalProofs[1].title = "";
  const q = blankQuestion("number");
  q.key = "buckets";
  q.title = "";
  q.min = "5";
  q.max = "2";
  const dep = { ...blankQuestion("text"), key: "why", title: "Why", onlyIfQuestion: "buckets", onlyIfValue: "x" };
  rows.removalQuestions = [q, dep];
  const problems = weighingProblems(rows);
  for (const want of [
    "Offer at least one way of weighing",
    "Default animals per day",
    "water video slot needs a title",
    "Removal question 1: needs the question text",
    "Removal question 1: min must not exceed max",
    'Removal question 2: "ask only when" must name a pick-one question',
    "per-animal video cannot be switched off",
    "at most 5",
  ]) {
    assert.ok(problems.some((p) => p.includes(want)), `missing problem containing: ${want}\n${problems.join("\n")}`);
  }
});

test("an authored question and a changed mode emit the wire shape the backend validates", () => {
  const rows = parseWeighing({ weighing: JSON.parse(readFileSync(seedPath, "utf8")) });
  rows.removalMode = "optional";
  rows.removalProofs[0].title = "Feed away";
  const q = blankQuestion("choice");
  q.key = "all_pens";
  q.title = "Every pen emptied?";
  q.options.push({ value: "other", label: "Other" });
  q.allowOther = true;
  rows.removalQuestions = [q];
  const out = emitWeighing(rows);
  assert.equal(out.feed_water_removal.mode, "optional");
  assert.equal(out.feed_water_removal.proofs[0].title, "Feed away");
  assert.equal(out.feed_water_removal.proofs[0].kind, "video");
  assert.deepEqual(out.feed_water_removal.questions[0], {
    id: "all_pens", kind: "choice", title: "Every pen emptied?", required: true,
    options: [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }, { value: "other", label: "Other" }], allow_other: true,
  });
  assert.deepEqual(weighingProblems(rows), []);
});
