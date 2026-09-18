import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const modulePageSource = readFileSync(new URL("./module-page.tsx", import.meta.url), "utf8");
const builderSource = readFileSync(new URL("./sop-builder.tsx", import.meta.url), "utf8");
const actionsSource = readFileSync(new URL("./sop-actions.ts", import.meta.url), "utf8");
const deriveSource = readFileSync(new URL("./sop-derive.ts", import.meta.url), "utf8");

test("module SOP pages author into their own slice", () => {
  assert.match(modulePageSource, /slice: SopScopeDomain/);
  assert.match(modulePageSource, /<SopBuilder[^>]+domain=\{slice\}/);
  assert.match(builderSource, /domain: SopScopeDomain;/);
  assert.doesNotMatch(builderSource, /const domain: SopScopeDomain = "vaccination"/);
  // Procurement joined the split on 2026-09-14 (Procurement SOP: the animal purchase inspection).
  assert.match(deriveSource, /export type SopScopeDomain = "vaccination" \| "counts" \| "feed" \| "milk" \| "weighing" \| "procurement" \| "general";/);
  assert.match(deriveSource, /const prefix = input\.domain;/);
  assert.match(actionsSource, /SOP_SLICE_LABEL\[input\.domain\]/);
});

// SHIFTING SOP (2026-09-16): the shifting SOP carries both a `shifting` cards section and the
// dormant seeded follow_up track; the cards editor must be chosen BEFORE the operator-steps editor.
test("the shifting cards editor is chosen before the follow_up editor", () => {
  const shiftingBranch = modulePageSource.indexOf("parseShifting(version.form_dsl)");
  const followUpBranch = modulePageSource.indexOf("parseFollowUp(version.form_dsl)");
  assert.ok(shiftingBranch > 0, "module page must parse the shifting section");
  assert.ok(followUpBranch > shiftingBranch, "parseShifting must run before parseFollowUp");
});
