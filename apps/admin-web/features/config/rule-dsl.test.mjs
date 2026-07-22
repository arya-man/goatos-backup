import { test } from "node:test";
import assert from "node:assert";
import {
  buildVaccinationMatrixPreview,
  newCapacityPolicy,
  newCompatibilityPolicy,
  newDose,
  newFeedFields,
  newPregnancyPolicy,
  newProcurementPolicy,
} from "./rule-dsl.ts";

test("new vaccination capacity defaults to 200 animals per operator per day", () => {
  const policy = newCapacityPolicy();
  assert.strictEqual(policy.maxPerDay, 200);
  assert.strictEqual(policy.maxBufferDays, 7);
});

test("buildVaccinationMatrixPreview omits wrapper course_type", () => {
  const dose = newDose(1);
  const input = {
    category: "vaccination",
    code: "vaccination.matrix",
    name: "Vaccination matrix",
    scope: "tenant:tenant-1",
    effectiveFrom: "2026-07-16",
    sopVersionId: "62000000-0000-4000-8000-000000000001",
    vaccine: {
      code: "FMD",
      name: "FMD",
      type: "killed",
      pathogenClass: "viral",
      courseType: "single",
      inventoryItemId: "item-fmd",
      manufacturer: "reviewed",
      disease: "FMD",
      compatibilityGroup: "FMD",
    },
    eligibility: {
      species: "goat",
      stage: "ADULT",
      sex: "all",
      breed: "all",
      lifecycle: "alive",
      health: "any",
      reproductive: "any",
      excludeReproductiveStates: ["pregnant_late"],
      deferStates: ["sick", "under_treatment", "recovering", "icu", "quarantine"],
    },
    vaccineLotPolicy: "required",
    missedDosePolicy: "immediate",
    escalation: "standard",
    compatibilityPolicy: newCompatibilityPolicy(),
    procurementPolicy: newProcurementPolicy(),
    pregnancyPolicy: newPregnancyPolicy(),
    capacityPolicy: newCapacityPolicy(),
    doses: [dose],
    feed: newFeedFields(),
  };
  const rows = [
    {
      id: "row-1",
      vaccine: input.vaccine,
      species: "goat",
      stage: "ADULT",
      sex: "all",
      breed: "all",
      doses: [dose],
    },
  ];

  const preview = buildVaccinationMatrixPreview(input, rows);
  assert.strictEqual(preview.vaccine.course_type, undefined);
  assert.strictEqual(preview.matrix_rows[0].vaccine.course_type, "single");
});
