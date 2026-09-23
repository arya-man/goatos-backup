// The shared primitive: one comparison, two verdict vocabularies.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { compareReadings, contractRevisionRefusal, isAbsentReading } from "./reading-comparison.mjs";

test("same conditions: a disagreement is something the page VARIES, not a finding", () => {
  const verdict = compareReadings(41, 42, { conditions: "same", label: "the total" });
  assert.equal(verdict.agreed, false);
  assert.match(verdict.verdict, /something the page varies, not something it owes/);
  assert.doesNotMatch(verdict.verdict, /moved when it should not/, "the two vocabularies crossed");
});

test("a deliberate action between them: the same disagreement is a FINDING", () => {
  const verdict = compareReadings(41, 42, { conditions: "deliberate-action", label: "the total" });
  assert.equal(verdict.agreed, false);
  assert.match(verdict.verdict, /moved when it should not/);
  assert.doesNotMatch(verdict.verdict, /something the page varies/, "the two vocabularies crossed");
});

test("agreement is agreement under either set of conditions", () => {
  for (const conditions of ["same", "deliberate-action"]) {
    const verdict = compareReadings(42, 42, { conditions });
    assert.equal(verdict.agreed, true);
    assert.equal(verdict.value, 42);
  }
});

test("the conditions are required — a comparison with no conditions has no verdict to give", () => {
  assert.throws(() => compareReadings(1, 1, {}), /needs to know the conditions/);
});

test("a reading that never arrived is refused rather than agreeing with another absence", () => {
  // Direction of failure: two absences are perfectly consistent, and consistency is how a value
  // gets promoted. Absence must push toward "not checked", never toward an expectation.
  assert.equal(compareReadings(null, null, { conditions: "same" }).agreed, false);
  assert.match(compareReadings(null, null, { conditions: "same" }).verdict, /never arrived/);
});

test("the sum and count reducers let a list and a number compare through one path", () => {
  assert.equal(compareReadings(["a", "b"], ["c", "d"], { conditions: "same", all: "count" }).agreed, true);
  assert.equal(compareReadings(["a", "b"], ["c"], { conditions: "same", all: "count" }).agreed, false);
  assert.equal(compareReadings([1, 2, 3], [3, 3], { conditions: "same", all: "sum" }).agreed, true);
});

test("a placeholder contract revision is refused, never defaulted", () => {
  for (const sha of ["unknown", "dev", "", "  ", undefined, null, "NONE"]) {
    assert.ok(contractRevisionRefusal(sha), `${JSON.stringify(sha)} was accepted as a build identity`);
  }
  assert.equal(contractRevisionRefusal("9f3c1ab"), null);
});

// ---------------------------------------------- nothing never equals nothing (builder A's finding)
test("two empty readings never agree, through every reducer", () => {
  // A measured this: with the page drawing nothing at both readings, sum reported and a single
  // cell reported, but COUNT PASSED — because count over an empty list is 0, and two zeroes agree.
  // The check therefore runs on the RAW readings, before any reducer.
  for (const all of [undefined, "sum", "count"]) {
    for (const conditions of ["same", "deliberate-action"]) {
      const verdict = compareReadings([], [], { conditions, all });
      assert.equal(verdict.agreed, false, `[] vs [] agreed with all=${all} under ${conditions}`);
      assert.match(verdict.verdict, /nothing is never an answer here/);
    }
  }
  assert.equal(compareReadings("", "", { conditions: "same" }).agreed, false);
  assert.equal(compareReadings({}, {}, { conditions: "same" }).agreed, false);
});

test("ZERO is not nothing — a real reading of zero still compares", () => {
  // The line that keeps this fix from becoming its own defect: a total that reads 0, a count of
  // 0 rows, or false are real readings and must keep comparing.
  assert.equal(compareReadings(0, 0, { conditions: "same" }).agreed, true);
  assert.equal(compareReadings(false, false, { conditions: "same" }).agreed, true);
  assert.equal(compareReadings([0], [0], { conditions: "same", all: "count" }).agreed, true);
  assert.equal(compareReadings([0], [0], { conditions: "same", all: "sum" }).agreed, true);
  assert.equal(compareReadings(0, 1, { conditions: "deliberate-action" }).agreed, false, "0 must still be able to MOVE");
});

test("isAbsentReading draws the line in one place", () => {
  for (const nothing of [null, undefined, "", "   ", [], {}]) assert.equal(isAbsentReading(nothing), true, `${JSON.stringify(nothing)}`);
  for (const something of [0, false, "0", [0], [""], { a: 1 }, NaN]) assert.equal(isAbsentReading(something), false, `${JSON.stringify(something)}`);
});

test("the shared definition states why agreement alone is not enough", () => {
  // Not decoration: every refusal in this file and its callers exists because of this one line,
  // and a later reader who removes one needs to find the reason in the same place as the rules.
  const source = readFileSync(new URL("./reading-comparison.mjs", import.meta.url), "utf8");
  assert.match(source, /CONSISTENCY IS NOT CORRECTNESS WHEN THE TWO THINGS BEING COMPARED ARE NOT THE SAME THING/);
  for (const instance of [/Two empty readings agree/, /sign-in page/, /source edit/, /principal label/]) {
    assert.match(source, instance, "an instance of the pattern is missing from the definition");
  }
});
