import assert from "node:assert/strict";
import { test } from "node:test";

import { displayStageLabel, fmtValuationSavedAt, storedStageLabel } from "./valuation-display.ts";

const register = [
  { code: "K0", label: "Newborn" },
  { code: "K1", label: "Milk training" },
  { code: "F2", label: "F2" },
];

test("a stage labelled with its raw code shows the register's name", () => {
  assert.equal(displayStageLabel("K0", "K0", register), "Newborn");
  assert.equal(displayStageLabel("K1", "", register), "Milk training");
  assert.equal(displayStageLabel("k0", "k0", register), "Newborn");
});

test("an authored label, an unnamed code, or a new row is left as is", () => {
  assert.equal(displayStageLabel("K0", "Kids 0-1m", register), "Kids 0-1m");
  assert.equal(displayStageLabel("K3", "K3", register), "K3");
  assert.equal(displayStageLabel("F2", "F2", register), "F2");
  assert.equal(displayStageLabel("", "", register), "");
});

test("saving keeps the stored label unless the farm edited the shown name", () => {
  assert.equal(storedStageLabel("K0", "K0", "Newborn", register), "K0");
  assert.equal(storedStageLabel("K0", "K0", "Newborn kids", register), "Newborn kids");
  assert.equal(storedStageLabel("K3", "K3", "K3", register), "K3");
});

test("Last saved reads DD/MM/YYYY HH:MM", () => {
  assert.equal(fmtValuationSavedAt("25-09-2026 13:05"), "25/09/2026 13:05");
  assert.equal(fmtValuationSavedAt("something else"), "something else");
});
