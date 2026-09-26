import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { stageLabel } from "../../lib/stage-labels.ts";

const pensTable = readFileSync(new URL("./counts-breakdown-pens-table.tsx", import.meta.url), "utf8");

// A reader must never see the legacy "F2" token (design rule, 2026-09-19). The page maps a stage
// code through `stageLabels`, which it builds from the response's OWN stage facet -- and that facet
// carries the FAMILY, "F2", while a pen row carries the SEXED code, "F2-Male". Every lookup for a
// sexed fattening pen therefore MISSES, and the miss used to fall through to the raw code: the
// "Head count by pen" table showed "F2-Male" in five pens (CBE Castro 1/2/3, CPT Godel 2 - Part
// 1/2) while `pointLabel` -- which already falls through `stageLabel` -- wrote "Fattening-Male" in
// the same table for a multi-stage pen. One page, one stage, two words.
//
// The guard is on the FALLBACK, not on the map: the map is only ever as complete as the facet, so
// what makes the rule hold is that a miss lands on the presentation helper rather than on raw text.
test("a stage the facet does not name still reaches the reader as Fattening, never F2", () => {
  // The rule the fallback has to satisfy, stated on the helper itself.
  assert.equal(stageLabel("F2-Male"), "Fattening-Male");
  assert.equal(stageLabel("F2-Female"), "Fattening-Female");
  assert.equal(stageLabel("ICU-F2-Male"), "ICU-Fattening-Male");
  // ...and codes that merely contain the letters are left alone.
  assert.equal(stageLabel("F21"), "F21");
  assert.equal(stageLabel("Non-Pregnant"), "Non-Pregnant");

  // Every `stageLabels.get(...)` in the pen table falls back through stageLabel(), never to the
  // raw code. Written as a shape check because the leak was three separate call sites and a
  // fourth would reintroduce it silently.
  const lookups = [...pensTable.matchAll(/stageLabels\.get\(([^)]*)\)\s*\?\?\s*([^\n]+?)(?:\)|\}|$)/g)];
  assert.ok(lookups.length >= 3, `expected the pen table's stage lookups, found ${lookups.length}`);
  for (const [whole, , fallback] of lookups) {
    assert.match(fallback.trim(), /^stageLabel\(/, `stage lookup falls back to a raw code: ${whole.trim()}`);
  }
});
