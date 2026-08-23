import assert from "node:assert/strict";
import test from "node:test";

import { fromRuleDsl, newVaccineToEditor, toRuleDsl } from "./editor-model.ts";

/**
 * "+ Add a vaccine" needs no new backend endpoint: appending a matrix row to
 * the same rule_dsl envelope the editor already writes back on save. These
 * pin the two things that would otherwise be easy to get wrong silently --
 * publish's own required schedule fields, and a booster reading as a second
 * kid dose the existing dose UI already knows how to show.
 */

const BASE_DOC = {
  eligibility: {
    sex: "all",
    breed: "all",
    lifecycle: "alive",
    health: "any",
    reproductive: "any",
    exclude_reproductive_states: ["pregnant_late"],
    defer_states: ["sick", "under_treatment", "recovering", "icu", "quarantine"],
  },
  matrix_rows: [
    {
      row_id: "et_tt",
      vaccine: { code: "ET+TT", name: "ET+TT", type: "killed" },
      schedule: [
        {
          dose_code: "et_tt_4w",
          sequence: 1,
          trigger_type: "birth_age",
          offset_days: 28,
          due_window_days: 7,
          dose_amount: 2,
          dose_unit: "ml",
          route_site: "subcutaneous",
          max_delay_days: 7,
          course_lapse_policy: "pc_review",
          repeat: "none",
        },
      ],
    },
  ],
  schedule: [],
};

test("newVaccineToEditor turns a booster course into two kid doses, second offset by the gap", () => {
  const v = newVaccineToEditor({
    name: "Brucella",
    code: "BRU",
    disease: "Brucellosis",
    vaccineType: "killed",
    pathogenClass: "bacterial",
    species: "both",
    courseType: "booster",
    firstDoseDays: 119, // 17 weeks
    boosterGapDays: 21,
    repeatDays: 365,
    maxLateDays: 14,
  });
  assert.equal(v.kidDoses.length, 2);
  assert.equal(v.kidDoses[0].offsetDays, 119);
  assert.equal(v.kidDoses[1].offsetDays, 140); // 119 + 21
  assert.equal(v.on, true);
  assert.equal(v.repeatDays, 365);
});

test("a first dose of 17 weeks and a deadline of 5 months both round-trip exactly", () => {
  const v = newVaccineToEditor({
    name: "Brucella",
    code: "BRU",
    disease: "",
    vaccineType: "killed",
    pathogenClass: "bacterial",
    species: "both",
    courseType: "single",
    firstDoseDays: 17 * 7,
    boosterGapDays: 0,
    repeatDays: null,
    maxLateDays: 5 * 30,
  });
  assert.equal(v.kidDoses[0].offsetDays, 119);
  assert.equal(v.maxLateDays, 150);
});

test("toRuleDsl appends a brand new matrix row with every field publish requires", () => {
  const plan = fromRuleDsl(BASE_DOC, null);
  const added = newVaccineToEditor({
    name: "Brucella",
    code: "BRU",
    disease: "Brucellosis",
    vaccineType: "killed",
    pathogenClass: "bacterial",
    species: "goat",
    courseType: "single",
    firstDoseDays: 84,
    boosterGapDays: 0,
    repeatDays: 365,
    maxLateDays: 14,
  });
  const nextPlan = { ...plan, vaccines: [...plan.vaccines, added] };

  const doc = toRuleDsl(BASE_DOC, nextPlan);

  assert.equal(doc.matrix_rows.length, 2, "the original row survives untouched, plus the new one");
  const row = doc.matrix_rows.find((r) => r.vaccine.code === "BRU");
  assert.ok(row, "the new vaccine got a matrix row");
  assert.equal(row.vaccine.type, "killed");
  assert.equal(row.vaccine.pathogen_class, "bacterial");
  assert.equal(row.vaccine.course_type, "single");
  assert.deepEqual(row.eligibility.species, ["goat"]);
  // Copied from the plan's own eligibility, not invented here.
  assert.equal(row.eligibility.sex, "all");
  assert.deepEqual(row.eligibility.defer_states, [
    "sick",
    "under_treatment",
    "recovering",
    "icu",
    "quarantine",
  ]);

  assert.equal(row.schedule.length, 2, "one kid dose, one repeat rule");
  const kid = row.schedule.find((s) => s.trigger_type === "birth_age");
  assert.equal(kid.offset_days, 84);
  assert.equal(kid.dose_amount, 1);
  assert.equal(kid.dose_unit, "ml");
  assert.equal(kid.route_site, "subcutaneous");
  assert.equal(kid.course_lapse_policy, "pc_review");
  assert.equal(kid.due_window_days, 14);
  assert.equal(kid.max_delay_days, 14);

  const repeat = row.schedule.find((s) => s.repeat === "every_n_days");
  assert.equal(repeat.offset_days, 365);

  // The flat top-level schedule is the union of every row, new one included.
  assert.equal(doc.schedule.length, 3);
});

test("an existing vaccine's own row is left untouched by adding a new one", () => {
  const plan = fromRuleDsl(BASE_DOC, null);
  const added = newVaccineToEditor({
    name: "Brucella",
    code: "BRU",
    disease: "",
    vaccineType: "live",
    pathogenClass: "viral",
    species: "both",
    courseType: "single",
    firstDoseDays: 84,
    boosterGapDays: 0,
    repeatDays: null,
    maxLateDays: 7,
  });
  const nextPlan = { ...plan, vaccines: [...plan.vaccines, added] };
  const doc = toRuleDsl(BASE_DOC, nextPlan);
  const original = doc.matrix_rows.find((r) => r.vaccine.code === "ET+TT");
  assert.equal(original.schedule[0].offset_days, 28);
  assert.equal(original.schedule[0].dose_amount, 2);
});
