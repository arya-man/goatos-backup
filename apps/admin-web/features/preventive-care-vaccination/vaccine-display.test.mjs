import assert from "node:assert/strict";
import test from "node:test";
import { vaccinationDriveDisplayName } from "../../lib/vaccine-display.ts";

test("vaccinationDriveDisplayName maps matrix schedule codes to human labels", () => {
  assert.equal(
    vaccinationDriveDisplayName("Preventive Care Vaccination Matrix - et_tt_adult_w2"),
    "ET+TT Adult course 2 weeks",
  );
  assert.equal(vaccinationDriveDisplayName("ppr_adult_w1"), "PPR Adult course 1 week");
});
