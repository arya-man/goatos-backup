import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { blankProofSlot, blankQuestion, emitWeighing, parseWeighing, weighingProblems } from "./weighing-model.ts";

// The seeded document is the same bytes migration 000315 adds to each tenant's published
// weighing.session version (pinned by the Go test TestMigrationEmbedsTheSeededWeighingSOP).
// Parsing it into editor rows and emitting it back must reproduce it EXACTLY, or opening the
// editor and pressing Publish with no edits would change what the planner and the phone run.
const seedPath = new URL("../../../../backend/internal/weighing/domain/sopseed/weighing_session.json", import.meta.url);

function canonical(v) {
  return JSON.stringify(v, (_k, val) => (val && typeof val === "object" && !Array.isArray(val) ? Object.fromEntries(Object.keys(val).sort().map((k) => [k, val[k]])) : val));
}

test("number question bounds cannot silently disappear when saving a draft or publishing", () => {
  const doc = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseWeighing({ weighing: doc });
  const question = { ...blankQuestion("number"), key: "count", title: "Count" };
  rows.removalQuestions = [question];
  for (const bound of ["min", "max"]) {
    for (const invalid of ["abc", "NaN", "Infinity", "-Infinity", "1e309"]) {
      question[bound] = invalid;
      assert.ok(weighingProblems(rows).includes(`Removal question 1: ${bound} must be a finite number`), `${bound}=${invalid} must block saving`);
    }
    question[bound] = "";
  }
  assert.deepEqual(weighingProblems(rows), []);
  assert.equal("min" in emitWeighing(rows).feed_water_removal.questions[0], false);
  assert.equal("max" in emitWeighing(rows).feed_water_removal.questions[0], false);
  question.min = "-2.5";
  question.max = "0";
  assert.deepEqual(weighingProblems(rows), []);
  const emitted = emitWeighing(rows).feed_water_removal.questions[0];
  assert.equal(emitted.min, -2.5);
  assert.equal(emitted.max, 0);
});

test("the seeded weighing rules round-trip through the editor model byte-faithfully", () => {
  const doc = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseWeighing({ weighing: doc });
  assert.ok(rows);
  assert.equal(rows.removalMode, "required");
  assert.deepEqual(rows.modes, ["individual_animal", "per_shed_partition"]);
  assert.deepEqual(rows.removalProofs.map((p) => [p.key, p.kind, p.required]), [["feed_video", "video", true], ["water_video", "video", true]]);
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
  rows.removalProofs.forEach((p) => { p.required = false; });
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
    "Capture 2: needs a title",
    "At least one capture must be compulsory",
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

test("authored capture slots: a photo beside the videos, a video swapped for a photo, an optional slot, a removed slot", () => {
  const rows = parseWeighing({ weighing: JSON.parse(readFileSync(seedPath, "utf8")) });
  rows.removalProofs[1].kind = "photo";
  rows.removalProofs[1].title = "Empty water trough";
  rows.removalProofs.push({ ...blankProofSlot(), key: "gate", title: "Gate closed", kind: "either", required: false });
  assert.deepEqual(weighingProblems(rows), []);
  const out = emitWeighing(rows);
  assert.deepEqual(out.feed_water_removal.proofs.map((p) => [p.key, p.kind, p.required]), [["feed_video", "video", true], ["water_video", "photo", true], ["gate", "either", false]]);
  // Round-trips.
  assert.equal(canonical(emitWeighing(parseWeighing({ weighing: out }))), canonical(out));
  // Drop every slot: refused while the removal is on; fine when it is off.
  rows.removalProofs = [];
  assert.ok(weighingProblems(rows).some((p) => p.includes("at least one capture")));
  rows.removalMode = "off";
  assert.deepEqual(weighingProblems(rows), []);
});

test("the removal evening: blank means the farm's, HH:MM is emitted, anything else is refused", () => {
  const rows = parseWeighing({ weighing: JSON.parse(readFileSync(seedPath, "utf8")) });
  assert.equal(rows.removalCutoffTime, "");
  assert.equal(emitWeighing(rows).feed_water_removal.cutoff_time, undefined);
  rows.removalCutoffTime = "21:30";
  assert.deepEqual(weighingProblems(rows), []);
  assert.equal(emitWeighing(rows).feed_water_removal.cutoff_time, "21:30");
  rows.removalCutoffTime = "9pm";
  assert.ok(weighingProblems(rows).some((p) => p.includes("time like 20:00")));
});

test("a capture slot without the required flag (a pre-flag document) is compulsory", () => {
  const rows = parseWeighing({ weighing: { schema_version: "goatos.sop-weighing.v1", planning: { modes: ["individual_animal"], default_cap_per_day: 100 }, feed_water_removal: { mode: "required", proofs: [{ key: "feed_video", title: "Feed", kind: "video" }, { key: "gate", title: "Gate", kind: "photo", required: false }], questions: [] }, capture: { individual: { video_required: true }, lump_sum: { video_min: 1, video_max: 5 } } } });
  assert.deepEqual(rows.removalProofs.map((p) => p.required), [true, false]);
  assert.deepEqual(weighingProblems(rows), []);
});

test("the Weights pages' window: fixed date or rolling days, never before the earliest day, seeded when absent", () => {
  const seed = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseWeighing({ weighing: seed });
  assert.equal(rows.weightsFromMode, "fixed_date");
  assert.equal(rows.weightsFromDate, "2026-08-03");
  assert.equal(rows.weightsEarliestDate, "2026-08-01");
  assert.deepEqual(emitWeighing(rows).weights_pages, { default_from_mode: "fixed_date", default_from_date: "2026-08-03", earliest_date: "2026-08-01" });

  // A document published before the block existed reads as the seed, exactly as the backend does.
  const { weights_pages: _dropped, ...older } = seed;
  const olderRows = parseWeighing({ weighing: older });
  assert.equal(olderRows.weightsFromDate, "2026-08-03");
  assert.equal(olderRows.weightsEarliestDate, "2026-08-01");

  const rolling = { ...rows, weightsFromMode: "rolling_days", weightsFromDays: "45" };
  assert.deepEqual(weighingProblems(rolling), []);
  assert.deepEqual(emitWeighing(rolling).weights_pages, { default_from_mode: "rolling_days", default_from_days: 45, earliest_date: "2026-08-01" });
  assert.ok(weighingProblems({ ...rolling, weightsFromDays: "0" }).some((p) => p.includes("1 to 3650")));
  assert.ok(weighingProblems({ ...rows, weightsFromDate: "2026-07-20" }).some((p) => p.includes("cannot be before the earliest day")));
  assert.ok(weighingProblems({ ...rows, weightsEarliestDate: "" }).some((p) => p.includes("earliest day the calendar offers")));
  assert.ok(weighingProblems({ ...rows, weightsFromDate: "3 Aug" }).some((p) => p.includes("pick the day the pages open from")));
});
