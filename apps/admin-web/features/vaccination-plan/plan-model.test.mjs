import assert from "node:assert/strict";
import test from "node:test";

import { readVaccines } from "./plan-model.ts";

test("a legacy matrix row superseded by active rules for the same vaccine is listed once (pr294 C11)", () => {
  const ruleDsl = {
    matrix_rows: [
      { vaccine: { code: "ZZ", name: "Z1+Z3" }, schedule: [{ dose_code: "zz_1", sequence: 1, repeat: "none" }] },
      { vaccine: { code: "PPR", name: "PPR" }, schedule: [{ dose_code: "ppr_1", sequence: 1, repeat: "none" }] },
    ],
  };
  const activeRules = [
    { dose_code: "z1_z3_kid_4w", sequence: 1, repeat: "none", eligibility_json: { vaccine: { code: "Z1_Z3", name: "Z1+Z3" } } },
  ];
  const groups = readVaccines(ruleDsl, activeRules);
  assert.deepEqual(groups.map((g) => g.name).sort(), ["PPR", "Z1+Z3"]);
  assert.equal(groups.find((g) => g.name === "Z1+Z3").code, "Z1_Z3");
});
