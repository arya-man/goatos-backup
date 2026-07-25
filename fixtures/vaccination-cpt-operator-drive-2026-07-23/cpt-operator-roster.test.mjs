import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const roster = JSON.parse(readFileSync(new URL("./cpt-operator-roster.json", import.meta.url), "utf8"));

test("CPT operator roster keeps Darshan as the default executable drive operator", () => {
  const operators = roster.operators ?? [];
  const darshan = operators.find((operator) => operator.code === "vaccination_operator_darshan");

  assert.ok(darshan, "Darshan Talwar must be present in the CPT vaccination operator roster");
  assert.equal(darshan.display_name, "Darshan Talwar");
  assert.equal(darshan.can_execute_vaccination, true);
  assert.equal(darshan.tier, "manager");
  assert.deepEqual(darshan.park_scope, ["CPT"]);
  assert.equal(roster.default_operator_assignment?.default_operator_code, "vaccination_operator_darshan");
});

test("CPT operator roster keeps all drive operators covered by shift config inputs", () => {
  assert.deepEqual(
    (roster.operators ?? []).map((operator) => operator.code).sort(),
    ["vaccination_operator_amit", "vaccination_operator_darshan", "vaccination_operator_sagar"],
  );
  for (const operator of roster.operators ?? []) {
    assert.match(operator.week_off, /^(monday|tuesday|wednesday|thursday|friday|saturday|sunday)$/);
    assert.ok(Number.isInteger(operator.shift_start_minute), `${operator.code}: shift_start_minute is required`);
    assert.ok(Number.isInteger(operator.shift_end_minute), `${operator.code}: shift_end_minute is required`);
    assert.ok(operator.animal_cap_per_day > 0, `${operator.code}: animal_cap_per_day is required`);
  }
});
