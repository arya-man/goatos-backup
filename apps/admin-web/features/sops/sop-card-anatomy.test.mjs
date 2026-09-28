import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: sop-card-logo-tile (TR2-P2-10). SOP cards showed a grey letter avatar ("C") and cut their
// fact captions ("video proof · Verify b…").
const library = readFileSync(new URL("./sop-library.tsx", import.meta.url), "utf8");

test("guard: sop-card-logo-tile - the JobItem logo slot is an icon tile and facts wrap", () => {
  const card = library.slice(library.indexOf("<JobItem"), library.indexOf("/>", library.indexOf("menuActions={[", library.indexOf("<JobItem"))));
  assert.doesNotMatch(card, /charAt\(0\)/);
  assert.match(card, /avatar=\{<Iconify/);
  assert.match(card, /\bwrapFacts\b/);
});
