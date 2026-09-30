// guard: sop-builder-e2e-selectors (FIXJ4, FIXJ3 hand-off). The SOP builder e2e drives the template
// UI through roles, labels and data-testid hooks; a selector on a legacy class that the template
// parts no longer render (.qtype select, .chipset .chip, .condrow, .cfgmodal, .btn, .tsearch,
// #sopCards, .screen, .alert.warn) silently makes a step a no-op or a timeout.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("sop-builder-e2e-selectors: no legacy class / id selectors", () => {
  const src = readFileSync(new URL("./sop-builder-e2e.mjs", import.meta.url), "utf8");
  for (const legacy of [".qtype", ".chipset", ".condrow", ".condval", ".cond-note", ".cfgmodal", ".btn", ".tsearch", "#sopCards", '".screen"', ".alert.warn", ".qfoot", ".ia.del", ".buildermain"]) {
    assert.ok(!src.includes(legacy), `sop-builder-e2e.mjs still selects ${legacy}`);
  }
  assert.doesNotMatch(src, /locator\(['"]select\[/, "native <select> selectors: the builder selects are MUI comboboxes");
});
