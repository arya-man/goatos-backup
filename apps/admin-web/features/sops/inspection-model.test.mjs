import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { emitInspection, flattenInspection, inspectionProblems, parseInspection, slugKey } from "./inspection-model.ts";

// The seeded document is the same bytes migration 000304 publishes (pinned by the Go test
// TestMigrationEmbedsTheSeededInspection). Parsing it into editor rows and emitting it back must
// reproduce it EXACTLY, or opening the editor and pressing Publish with no edits would change
// what the phone runs.
const seedPath = new URL("../../../../backend/internal/animalpurchase/domain/inspectionseed/animal_purchase.json", import.meta.url);

function canonical(v) {
  return JSON.stringify(v, (_k, val) => (val && typeof val === "object" && !Array.isArray(val) ? Object.fromEntries(Object.keys(val).sort().map((k) => [k, val[k]])) : val));
}

test("the seeded inspection round-trips through the editor model byte-faithfully", () => {
  const doc = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseInspection({ inspection: doc });
  assert.ok(rows);
  assert.equal(rows.pages.length, 5);
  assert.deepEqual(rows.loadForm.map((q) => q.key), ["load_ref", "vendor", "farm", "expected_count", "notes"]);
  assert.equal(canonical(emitInspection(rows)), canonical(doc));
  assert.deepEqual(inspectionProblems(rows), []);
  assert.equal(flattenInspection(rows).length, 38);
});

test("pre-checks name the field: duplicate key, missing choices, bad only_if, removed locked question", () => {
  const rows = parseInspection({ inspection: JSON.parse(readFileSync(seedPath, "utf8")) });
  const p0 = rows.pages[0];
  p0.questions.push({ ...p0.questions[1], id: "x", key: "goat_id" });
  p0.questions.push({ ...p0.questions[0], id: "y", key: "empty_choice", options: [] });
  p0.questions.push({ ...p0.questions[0], id: "z", key: "dep", options: [{ value: "yes", label: "Yes" }], onlyIfQuestion: "field_verdict", onlyIfValue: "selected" });
  rows.pages[4].questions = rows.pages[4].questions.filter((q) => q.key !== "breed");
  const problems = inspectionProblems(rows);
  for (const want of ['"goat_id" is used twice', "needs at least one choice", "must name an earlier question", 'reads "breed"']) {
    assert.ok(problems.some((p) => p.includes(want)), `${want}\n${problems.join("\n")}`);
  }
});

test("a media question emits accepts by capture kind and defaults its slot to its key", () => {
  const rows = parseInspection({ inspection: JSON.parse(readFileSync(seedPath, "utf8")) });
  rows.pages[0].questions.push({ id: "m", key: "hoof_photo", kind: "media", title: "Hoof photo", hint: "", required: true, options: [], allowOther: false, slot: "", maxFiles: 2, accepts: "photo", min: "", max: "", unit: "", onlyIfQuestion: "", onlyIfValue: "" });
  const emitted = emitInspection(rows);
  const q = emitted.pages[0].questions.at(-1);
  assert.deepEqual(q, { id: "hoof_photo", kind: "media", title: "Hoof photo", required: true, slot: "hoof_photo", max_files: 2, accepts: ["photo"] });
  assert.equal(slugKey("Hoof photo (left)", new Set(["hoof_photo_left"])), "hoof_photo_left_2");
});

test("the load form: extra questions are authored, media refused, identity stays compulsory", () => {
  const rows = parseInspection({ inspection: JSON.parse(readFileSync(seedPath, "utf8")) });
  rows.loadForm.push({ id: "t", key: "transport", kind: "choice", title: "How did the load arrive?", hint: "", required: true, options: [{ value: "truck", label: "Truck" }, { value: "walk", label: "On foot" }], allowOther: false, slot: "", maxFiles: 0, accepts: "both", min: "", max: "", unit: "", onlyIfQuestion: "", onlyIfValue: "" });
  assert.deepEqual(inspectionProblems(rows), []);
  const emitted = emitInspection(rows);
  assert.equal(emitted.load_form.questions.length, 6);
  assert.deepEqual(emitted.load_form.questions[5], { id: "transport", kind: "choice", title: "How did the load arrive?", required: true, options: [{ value: "truck", label: "Truck" }, { value: "walk", label: "On foot" }] });
  rows.loadForm.push({ ...rows.loadForm[5], id: "m", key: "truck_photo", kind: "media", options: [] });
  rows.loadForm.find((q) => q.key === "farm").required = false;
  const problems = inspectionProblems(rows);
  assert.ok(problems.some((p) => p.includes("recorded per animal")) && problems.some((p) => p.includes('"farm" must stay compulsory')), problems.join("\n"));
});
