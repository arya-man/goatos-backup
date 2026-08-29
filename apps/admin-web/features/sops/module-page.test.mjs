import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const modulePageSource = readFileSync(new URL("./module-page.tsx", import.meta.url), "utf8");
const builderSource = readFileSync(new URL("./sop-builder.tsx", import.meta.url), "utf8");
const actionsSource = readFileSync(new URL("./sop-actions.ts", import.meta.url), "utf8");
const deriveSource = readFileSync(new URL("./sop-derive.ts", import.meta.url), "utf8");

test("module SOP pages author into their own slice", () => {
  assert.match(modulePageSource, /slice: SopSliceDomain/);
  assert.match(modulePageSource, /<SopBuilder[^>]+domain=\{slice\}/);
  assert.match(builderSource, /domain: SopSliceDomain;/);
  assert.doesNotMatch(builderSource, /const domain: SopSliceDomain = "vaccination"/);
  assert.match(deriveSource, /export type SopSliceDomain = "vaccination" \| "counts" \| "feed" \| "milk" \| "weighing";/);
  assert.match(deriveSource, /const prefix = input\.domain;/);
  assert.match(actionsSource, /SOP_SLICE_LABEL\[input\.domain\]/);
});
