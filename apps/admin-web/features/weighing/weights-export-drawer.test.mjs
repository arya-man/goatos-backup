import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: weights-export-drawer (J2 P2-1). The Weights/ADG export drawer is a form: template form
// width 480 (never 360), and the period is named once, by the range field's own label.
const src = readFileSync(new URL("./weights-export.tsx", import.meta.url), "utf8");

test("guard: weights-export-drawer - 480 wide, one Period label", () => {
  assert.match(src, /<MinimalDrawer[\s\S]{0,200}width=\{480\}/);
  assert.equal((src.match(/"export\.period\.label"/g) ?? []).length, 1, "Period is the range field label only");
});
