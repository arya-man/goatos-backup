import assert from "node:assert/strict";
import { test } from "node:test";

import { stageLabel } from "./stage-labels.ts";

test("F2 token reads Fattening, the rest of the code is kept", () => {
  assert.equal(stageLabel("F2"), "Fattening");
  assert.equal(stageLabel("F2-Male"), "Fattening-Male");
  assert.equal(stageLabel("f2-female"), "Fattening-female");
  assert.equal(stageLabel("ICU-F2-Male"), "ICU-Fattening-Male");
  assert.equal(stageLabel("F2 · Shed 4"), "Fattening · Shed 4");
});

test("other words and ids are untouched", () => {
  assert.equal(stageLabel("Non-Pregnant"), "Non-Pregnant");
  assert.equal(stageLabel("K2"), "K2");
  assert.equal(stageLabel("GF2001"), "GF2001");
  assert.equal(stageLabel("F20"), "F20");
  assert.equal(stageLabel(""), "");
  assert.equal(stageLabel(null), "");
});
