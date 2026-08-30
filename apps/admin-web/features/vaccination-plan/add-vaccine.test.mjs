import assert from "node:assert/strict";
import test from "node:test";

import { fromRuleDsl, newVaccineToEditor, sanitizeRuleDslForSave, toRuleDsl } from "./editor-model.ts";
import { describeChange, readVaccines } from "./plan-model.ts";

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

test("readVaccines ignores null matrix rows from older imported plans", () => {
  const vaccines = readVaccines({
    ...BASE_DOC,
    matrix_rows: [null, ...BASE_DOC.matrix_rows],
  });

  assert.equal(vaccines.length, 1);
  assert.equal(vaccines[0].code, "ET+TT");
});

test("readVaccines lets active derived rules override stale matrix schedule display", () => {
  const vaccines = readVaccines(
    {
      matrix_rows: [
        {
          row_id: "real-seed-hs",
          vaccine: { code: "HS", name: "HS", type: "killed" },
          schedule: [{ dose_code: "hs_kid_16w", sequence: 9, trigger_type: "birth_age", offset_days: 84 }],
        },
      ],
    },
    [
      {
        rule_id: "rule-hs",
        protocol_version_id: "v1",
        protocol_id: "p1",
        dose_code: "hs_kid_12w",
        sequence: 9,
        trigger_type: "birth_age",
        offset_days: 84,
        repeat: "none",
        eligibility_json: { vaccine: { code: "HS", name: "HS", type: "killed" } },
      },
      {
        rule_id: "rule-ppr",
        protocol_version_id: "v1",
        protocol_id: "p1",
        dose_code: "ppr_kid_16w",
        sequence: 6,
        trigger_type: "birth_age",
        offset_days: 112,
        repeat: "none",
        eligibility_json: { vaccine: { code: "PPR", name: "PPR", type: "live" } },
      },
    ],
  );

  const hs = vaccines.find((item) => item.code === "HS");
  const ppr = vaccines.find((item) => item.code === "PPR");
  assert.equal(hs?.firstDoses[0]?.dose_code, "hs_kid_12w");
  assert.equal(hs?.firstDoses[0]?.offset_days, 84);
  assert.equal(ppr?.inPlan, true);
  assert.equal(ppr?.firstDoses[0]?.offset_days, 112);
});

test("newVaccineToEditor turns a booster course into kid timing plus adult follow-up", () => {
  const v = newVaccineToEditor({
    name: "Brucella",
    code: "BRU",
    disease: "Brucellosis",
    vaccineType: "killed",
    pathogenClass: "bacterial",
    species: "both",
    procurementPurpose: "all",
    courseType: "booster",
    firstDoseDays: 119, // 17 weeks
    boosterGapDays: 21,
    repeatDays: 365,
    maxLateDays: 14,
  });
  assert.equal(v.kidDoses.length, 2);
  assert.equal(v.kidDoses[0].offsetDays, 119);
  assert.equal(v.kidDoses[1].offsetDays, 140); // 119 + 21
  assert.equal(v.driveDoses.length, 2);
  assert.equal(v.driveDoses[0].triggerType, "manual_campaign");
  assert.equal(v.driveDoses[1].triggerType, "after_previous_completion");
  assert.equal(v.driveDoses[1].offsetDays, 21);
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
    procurementPurpose: "all",
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
    procurementPurpose: "all",
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

  assert.equal(row.schedule.length, 3, "one kid dose, one adult drive dose, one repeat rule");
  const kid = row.schedule.find((s) => s.trigger_type === "birth_age");
  assert.equal(kid.offset_days, 84);
  assert.equal(kid.dose_amount, 1);
  assert.equal(kid.dose_unit, "ml");
  assert.equal(kid.route_site, "subcutaneous");
  assert.equal(kid.course_lapse_policy, "pc_review");
  assert.equal(kid.due_window_days, 14);
  assert.equal(kid.max_delay_days, 14);

  const adult = row.schedule.find((s) => s.trigger_type === "manual_campaign");
  assert.equal(adult.offset_days, 84);
  assert.equal(adult.dose_code, "bru_adult_w1");
  assert.equal(adult.repeat, "none");
  assert.equal(adult.due_window_days, 14);

  const repeat = row.schedule.find((s) => s.repeat === "every_n_days");
  assert.equal(repeat.offset_days, 365);

  // The flat top-level schedule is the union of every row, new one included.
  assert.equal(doc.schedule.length, 4);
});

test("an existing vaccine's own row is left untouched by adding a new one", () => {
  const baseWithLongRepeatWindow = {
    ...BASE_DOC,
    matrix_rows: [
      {
        ...BASE_DOC.matrix_rows[0],
        schedule: [
          ...BASE_DOC.matrix_rows[0].schedule,
          {
            dose_code: "et_tt_revac",
            sequence: 2,
            trigger_type: "after_previous_completion",
            offset_days: 182,
            min_gap_days: 182,
            repeat: "every_n_days",
            catch_up: "next_cycle",
            due_window_days: 30,
            max_delay_days: 30,
            dose_amount: 2,
            dose_unit: "ml",
            route_site: "subcutaneous",
            course_lapse_policy: "pc_review",
          },
        ],
      },
    ],
  };
  const before = structuredClone(baseWithLongRepeatWindow.matrix_rows[0]);
  const plan = fromRuleDsl(baseWithLongRepeatWindow, null);
  const added = newVaccineToEditor({
    name: "Brucella",
    code: "BRU",
    disease: "",
    vaccineType: "live",
    pathogenClass: "viral",
    species: "both",
    procurementPurpose: "all",
    courseType: "single",
    firstDoseDays: 84,
    boosterGapDays: 0,
    repeatDays: null,
    maxLateDays: 7,
  });
  const nextPlan = { ...plan, vaccines: [...plan.vaccines, added] };
  const doc = toRuleDsl(baseWithLongRepeatWindow, nextPlan);
  const original = doc.matrix_rows.find((r) => r.vaccine.code === "ET+TT");
  assert.deepEqual(original, before);
});

// A short code that differs only in punctuation derives the same row id as an existing row, and
// publish rejects a matrix whose row ids repeat. Refusing the vaccine over a detail the author
// cannot see would be worse than suffixing the id, so the id moves and the typed code does not.
test("a new vaccine never reuses an existing matrix row id, and is still publishable", () => {
  const plan = fromRuleDsl(BASE_DOC, null);
  const existingRowId = BASE_DOC.matrix_rows[0].row_id;
  // A short code differing from the existing one only in punctuation derives the SAME row id, and
  // publish rejects a matrix whose row ids repeat. Refusing the vaccine over a detail the author
  // cannot see would be worse than suffixing the id, so the id moves and the typed code does not.
  const added = newVaccineToEditor({
    name: "Enterotoxaemia Tetanus Repeat",
    code: existingRowId.toUpperCase(),
    disease: "Enterotoxaemia",
    vaccineType: "killed",
    pathogenClass: "bacterial",
    species: "both",
    procurementPurpose: "all",
    courseType: "single",
    firstDoseDays: 28,
    boosterGapDays: 0,
    repeatDays: 180,
    maxLateDays: 7,
  });
  const doc = toRuleDsl(BASE_DOC, { ...plan, vaccines: [...plan.vaccines, added] });

  const ids = doc.matrix_rows.map((r) => r.row_id);
  assert.equal(new Set(ids).size, ids.length, `row ids collided: ${ids.join(", ")}`);
  assert.ok(ids.includes(existingRowId), "the existing row lost its id");

  const addedRow = doc.matrix_rows.find((r) => r.vaccine.code === existingRowId.toUpperCase());
  assert.ok(addedRow, "the added vaccine produced no row");
  assert.notEqual(addedRow.row_id, existingRowId, "the added row reused the existing row id");

  // Distinct row ids alone would be a hollow assertion: a row that publish rejects for any OTHER
  // missing field is just as unusable. So the row is checked against what publish.go requires of
  // an individual vaccine, which is what "no collision" is supposed to be worth.
  assert.equal(addedRow.vaccine.type, "killed", "vaccine.type missing -- publish requires it");
  assert.equal(addedRow.vaccine.pathogen_class, "bacterial", "pathogen_class missing -- publish requires it");
  assert.equal(addedRow.vaccine.course_type, "single", "course_type missing -- publish requires it");
  assert.ok(addedRow.vaccine.name, "vaccine.name missing -- publish requires it");
  assert.ok(addedRow.schedule.length > 0, "publish requires at least one schedule row");
  for (const row of addedRow.schedule) {
    assert.ok(row.dose_amount > 0, "dose_amount must be positive");
    assert.ok(row.dose_unit, "dose_unit required");
    assert.ok(row.route_site, "route_site required");
    assert.ok(row.course_lapse_policy, "course_lapse_policy required");
    assert.ok(row.max_delay_days >= row.due_window_days, "max_delay_days must cover the due window");
  }
  for (const key of ["sex", "breed", "lifecycle", "health", "reproductive"]) {
    assert.ok(addedRow.eligibility[key], `eligibility.${key} required by publish`);
  }
});

test("a new vaccine can target breeding procurement animals only", () => {
  const plan = fromRuleDsl(BASE_DOC, null);
  const added = newVaccineToEditor({
    name: "Breeding Fever",
    code: "BFV",
    disease: "Breeding fever",
    vaccineType: "killed",
    pathogenClass: "bacterial",
    species: "goat",
    procurementPurpose: "breeding",
    courseType: "single",
    firstDoseDays: 84,
    boosterGapDays: 0,
    repeatDays: null,
    maxLateDays: 14,
  });
  const doc = toRuleDsl(BASE_DOC, { ...plan, vaccines: [...plan.vaccines, added] });
  const row = doc.matrix_rows.find((r) => r.vaccine.code === "BFV");

  assert.deepEqual(row.eligibility.procurement_purpose, ["breeding"]);
});

test("all-purpose newly added vaccine rows stay unrestricted when the plan has a default purpose", () => {
  const plan = fromRuleDsl(
    {
      ...BASE_DOC,
      procurement_policy: { procurement_purpose: "fattening" },
    },
    null,
  );
  assert.equal(plan.procurement.procurementPurpose, "fattening");

  const added = newVaccineToEditor({
    name: "Purpose Default",
    code: "PDF",
    disease: "Purpose default",
    vaccineType: "killed",
    pathogenClass: "bacterial",
    species: "goat",
    procurementPurpose: "all",
    courseType: "single",
    firstDoseDays: 84,
    boosterGapDays: 0,
    repeatDays: null,
    maxLateDays: 14,
  });
  const doc = toRuleDsl(BASE_DOC, { ...plan, vaccines: [...plan.vaccines, added] });
  const row = doc.matrix_rows.find((r) => r.vaccine.code === "PDF");

  assert.equal(row.eligibility.procurement_purpose, undefined);
  assert.equal(doc.procurement_policy.procurement_purpose, "fattening");
});

test("procurement holding stores separate breeding and fattening wave choices", () => {
  const plan = fromRuleDsl(BASE_DOC, null);
  const doc = toRuleDsl(BASE_DOC, {
    ...plan,
    vaccines: [
      ...plan.vaccines,
      namedVaccine("Z1+Z3", "ZZ"),
      namedVaccine("PPR", "PPR"),
      namedVaccine("FMD", "FMD"),
      namedVaccine("HS", "HS"),
      namedVaccine("Goat Pox", "GP"),
      namedVaccine("Sheep Pox", "SP"),
    ],
    procurement: {
      ...plan.procurement,
      purposePlans: {
        breeding: {
          firstWave: ["ET+TT", "Z1+Z3"],
          secondWaveAfterDays: 28,
          goatSecondWave: ["Goat Pox"],
          sheepSecondWave: ["Sheep Pox"],
        },
        fattening: {
          firstWave: ["PPR"],
          secondWaveAfterDays: 14,
          goatSecondWave: ["FMD", "HS"],
          sheepSecondWave: ["ET+TT"],
        },
      },
    },
  });

  assert.deepEqual(doc.procurement_policy.purpose_plans.breeding.first_wave, ["ET+TT", "Z1+Z3"]);
  assert.deepEqual(doc.procurement_policy.purpose_plans.fattening.first_wave, ["PPR"]);
  assert.deepEqual(doc.procurement_policy.purpose_plans.fattening.goat_second_wave, ["FMD", "HS"]);

  const readBack = fromRuleDsl(doc, null);
  assert.deepEqual(readBack.procurement.purposePlans.breeding.firstWave, ["ET+TT", "Z1+Z3"]);
  assert.deepEqual(readBack.procurement.purposePlans.fattening.firstWave, ["PPR"]);
  assert.deepEqual(readBack.procurement.purposePlans.fattening.goatSecondWave, ["FMD", "HS"]);
});

test("procurement holding prunes wave choices for vaccines that are not in this plan", () => {
  const plan = fromRuleDsl(BASE_DOC, null);
  const doc = toRuleDsl(BASE_DOC, {
    ...plan,
    vaccines: [...plan.vaccines, { ...namedVaccine("PPR", "PPR"), on: false }],
    procurement: {
      ...plan.procurement,
      purposePlans: {
        ...plan.procurement.purposePlans,
        fattening: {
          firstWave: ["PPR", "ET+TT"],
          secondWaveAfterDays: 14,
          goatSecondWave: ["Missing Vaccine"],
          sheepSecondWave: [],
        },
      },
    },
  });

  assert.deepEqual(doc.procurement_policy.purpose_plans.fattening.first_wave, ["ET+TT"]);
  assert.deepEqual(doc.procurement_policy.purpose_plans.fattening.goat_second_wave, []);
});

function namedVaccine(name, code) {
  return newVaccineToEditor({
    name,
    code,
    disease: name,
    vaccineType: "killed",
    pathogenClass: "bacterial",
    species: "both",
    procurementPurpose: "all",
    courseType: "single",
    firstDoseDays: 84,
    boosterGapDays: 0,
    repeatDays: null,
    maxLateDays: 14,
  });
}

test("history change notes use the current display name for an added vaccine code", () => {
  const previous = readVaccines(BASE_DOC);
  const oldName = readVaccines({
    ...BASE_DOC,
    matrix_rows: [
      ...BASE_DOC.matrix_rows,
      {
        row_id: "z13",
        vaccine: { code: "Z13", name: "Z1Z3", type: "killed" },
        schedule: [
          {
            dose_code: "z13_12w",
            sequence: 1,
            trigger_type: "birth_age",
            offset_days: 84,
            due_window_days: 7,
            max_delay_days: 7,
            repeat: "none",
          },
        ],
      },
    ],
  });

  const note = describeChange(oldName, previous, new Map([["Z13", "Z1+Z3"]]));

  assert.equal(note, "Added Z1+Z3.");
});

test("draft save strips unsupported top-level rule_dsl notes instead of publishing them", () => {
  const doc = sanitizeRuleDslForSave({
    ...BASE_DOC,
    notes: [{ text: "legacy import note that publish must not see" }],
  });

  assert.equal(Object.hasOwn(doc, "notes"), false);
  assert.ok(Array.isArray(doc.matrix_rows));
});
