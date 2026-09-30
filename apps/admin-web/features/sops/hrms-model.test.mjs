import assert from "node:assert/strict";
import test from "node:test";

import { emitHrms, freeTriggers, keyFromTitle, parseHrms } from "./hrms-model.ts";

const seed = {
  schema_version: "goatos.sop-form.v1",
  fields: [],
  violations: {
    schema_version: "goatos.sop-hrms-violations.v1",
    violation_types: [{ key: "late_to_shift", title: "Late to shift", default_fine: 100, active: true }],
    enquiries: [{ trigger: "animal_death", title: "Death enquiry", due_hours: 48, questions: [{ id: "what_happened", kind: "text", title: "What happened", required: true }] }],
  },
};

test("a stored document round-trips unchanged", () => {
  const rows = parseHrms(seed);
  assert.deepEqual(emitHrms(rows), seed.violations);
});

test("a renamed stored type keeps its key; a new type gets one from its name", () => {
  const rows = parseHrms(seed);
  rows.types[0].title = "Late for shift";
  rows.types.push({ key: "", title: "Late to shift!", defaultFine: "250", active: true, stored: false });
  const out = emitHrms(rows);
  assert.equal(out.violation_types[0].key, "late_to_shift");
  assert.equal(out.violation_types[1].key, "late_to_shift_2");
  assert.equal(out.violation_types[1].default_fine, 250);
});

test("a retired type is kept, marked not in use", () => {
  const rows = parseHrms(seed);
  rows.types[0].active = false;
  assert.equal(emitHrms(rows).violation_types[0].active, false);
});

test("blank numbers go to the backend as null, never an invented value", () => {
  const rows = parseHrms(seed);
  rows.types[0].defaultFine = "";
  rows.enquiries[0].dueHours = " ";
  const out = emitHrms(rows);
  assert.equal(out.violation_types[0].default_fine, null);
  assert.equal(out.enquiries[0].due_hours, null);
});

test("new questions get ids; keys never start with a digit", () => {
  const rows = parseHrms(seed);
  rows.enquiries[0].questions.push({ id: "", kind: "yes_no", title: "24 hours watched?", required: false, stored: false });
  const q = emitHrms(rows).enquiries[0].questions[1];
  assert.equal(q.id, "question_24_hours_watched");
  assert.equal(keyFromTitle("", new Set(), "violation"), "violation");
});

test("an event with an enquiry is not offered again", () => {
  assert.deepEqual(freeTriggers(parseHrms(seed)), []);
  assert.equal(parseHrms({ fields: [] }), null);
});
