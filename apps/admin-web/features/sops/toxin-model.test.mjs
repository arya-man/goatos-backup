import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { emitToxin, parseToxin, toxinProblems, toxinWorkingStepCount } from "./toxin-model.ts";

const seed = JSON.parse(readFileSync(new URL("../../../../backend/internal/toxin/domain/toxinseed/toxin_test.json", import.meta.url), "utf8"));

// The round trip is the contract: opening the seeded procedure and saving it unchanged must
// produce the SAME document, or every author who opens the editor silently rewrites the test the
// farm runs.
test("the seeded procedure round-trips byte for byte", () => {
  const rows = parseToxin({ toxin: seed });
  assert.ok(rows, "seeded document must parse");
  assert.deepEqual(emitToxin(rows), seed);
});

test("a version with no procedure parses to null", () => {
  assert.equal(parseToxin({ follow_up: {} }), null);
  assert.equal(parseToxin({ toxin: { schema_version: "something.else", steps: [] } }), null);
});

test("the working step count ignores the waiting rows", () => {
  const rows = parseToxin({ toxin: seed });
  assert.equal(rows.steps.length, 7);
  assert.equal(toxinWorkingStepCount(rows), 6);
});

test("the client preview refuses what the engine refuses", () => {
  const t = (k) => k;
  const base = () => parseToxin({ toxin: JSON.parse(JSON.stringify(seed)) });

  assert.deepEqual(toxinProblems(base(), t), []);

  const noReading = base();
  noReading.steps[6].kind = "video";
  assert.ok(toxinProblems(noReading, t).includes("tsop.problem.one_reading"));

  const readingNotLast = base();
  readingNotLast.steps.push({ ...readingNotLast.steps[0], id: "x", kind: "video", title: "After", instruction: "After" });
  assert.ok(toxinProblems(readingNotLast, t).includes("tsop.problem.reading_last"));

  const forwardGate = base();
  forwardGate.steps[4].gateAfterStep = 6;
  assert.ok(toxinProblems(forwardGate, t).some((p) => p.endsWith("tsop.problem.gate_backwards")));

  const gateOnWait = base();
  gateOnWait.steps[4].gateAfterStep = 4;
  assert.ok(toxinProblems(gateOnWait, t).some((p) => p.endsWith("tsop.problem.gate_working")));

  const noWait = base();
  noWait.steps[3].waitMinutes = 0;
  assert.ok(toxinProblems(noWait, t).some((p) => p.endsWith("tsop.problem.wait_minutes")));

  const noInstruction = base();
  noInstruction.steps[0].instruction = "  ";
  assert.ok(toxinProblems(noInstruction, t).some((p) => p.endsWith("tsop.problem.instruction")));
});
