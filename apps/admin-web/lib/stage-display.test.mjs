import assert from "node:assert/strict";
import { test } from "node:test";

import { stageDisplayLabel, stageNameMap, stageVocabularyLabel } from "./stage-display.ts";

const names = stageNameMap([
  { stage_code: "K0", name: "Newborn" },
  { stage_code: "K3", name: "Weaned kids" },
  { stage_code: "F2", name: "Fattening" },
  { stage_code: "Buck", name: "" },
]);

test("stageDisplayLabel maps only the fattening family", () => {
  assert.equal(stageDisplayLabel("F2", names), "Fattening");
  assert.equal(stageDisplayLabel("K0", names), "K0");
});

test("stageVocabularyLabel reads every configured stage word, keeps codes without one, leaves non-stage labels alone", () => {
  assert.equal(stageVocabularyLabel("K0", names), "Newborn");
  assert.equal(stageVocabularyLabel("k3", names), "Weaned kids");
  assert.equal(stageVocabularyLabel("F2", names), "Fattening");
  assert.equal(stageVocabularyLabel("K1", names), "K1");
  assert.equal(stageVocabularyLabel("Buck", names), "Buck");
  assert.equal(stageVocabularyLabel("Adult females", names), "Adult females");
  assert.equal(stageVocabularyLabel("K0", stageNameMap(undefined)), "K0");
});
