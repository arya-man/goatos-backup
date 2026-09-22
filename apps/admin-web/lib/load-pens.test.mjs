import test from "node:test";
import assert from "node:assert/strict";

import {
  LOAD_PEN_BRACKET_MAX,
  loadPenBracket,
  loadPenNames,
  pensFromPlacements,
  withLoadPens,
} from "./load-pens.ts";

test("names are park-qualified, biggest placement first, deduped", () => {
  assert.deepEqual(
    loadPenNames([
      { park: "CPT", pen: "Castro 1", animals: 31 },
      { park: "CPT", pen: "Castro 2", animals: 32 },
      { park: "CPT", pen: "Castro 2", animals: 32 },
    ]),
    ["CPT Castro 2", "CPT Castro 1"],
  );
});

// THE PARK IS LOAD-BEARING (maintainer, 2026-09-05): "Castro 1" is a real pen in BOTH parks, so
// an unqualified bracket on a chart that mixes them names a pen that could be either — the OL-1
// name-merge defect.
test("the same pen name in two parks stays two pens", () => {
  assert.deepEqual(
    loadPenNames([
      { park: "CBE", pen: "Castro 1", animals: 54 },
      { park: "CPT", pen: "Castro 1", animals: 31 },
    ]),
    ["CBE Castro 1", "CPT Castro 1"],
  );
});

test("a pen whose park is unknown is still named, never dropped and never leading-spaced", () => {
  assert.equal(loadPenBracket([{ park: "", pen: "Yashoda 10" }]), " (Yashoda 10)");
  assert.equal(loadPenBracket([{ park: "  ", pen: " Castro 3 " }]), " (Castro 3)");
});

test("the bracket counts the pens it cannot spell out instead of dropping them", () => {
  const pens = [
    { park: "CBE", pen: "Castro 1", animals: 54 },
    { park: "CBE", pen: "Castro 2", animals: 40 },
    { park: "CBE", pen: "Castro 3", animals: 30 },
    { park: "CBE", pen: "Yashoda 10", animals: 2 },
  ];
  assert.equal(loadPenBracket(pens), " (CBE Castro 1, CBE Castro 2 +2)");
  assert.equal(
    loadPenBracket(pens, Number.POSITIVE_INFINITY),
    " (CBE Castro 1, CBE Castro 2, CBE Castro 3, CBE Yashoda 10)",
  );
  assert.equal(LOAD_PEN_BRACKET_MAX, 2);
});

// THE BRACKET IS NEVER INVENTED. A load with no known pen must render exactly as it did before
// this helper existed — no empty parentheses, no trailing space, no guess at the park.
test("no known pen means no bracket at all", () => {
  assert.equal(loadPenBracket(null), "");
  assert.equal(loadPenBracket([]), "");
  assert.equal(loadPenBracket([{ park: "CBE", pen: "   " }]), "");
  assert.equal(withLoadPens("131 · Krishnamorrthy", undefined), "131 · Krishnamorrthy");
});

test("withLoadPens appends to the caller's own label", () => {
  assert.equal(
    withLoadPens("Load 129 · Krishnamorrthy", [
      { park: "CPT", pen: "Godel 2 - Part 1", animals: 38 },
      { park: "CPT", pen: "Godel 2 - Part 2", animals: 38 },
    ]),
    "Load 129 · Krishnamorrthy (CPT Godel 2 - Part 1, CPT Godel 2 - Part 2)",
  );
});

test("pensFromPlacements passes the backend-composed display through verbatim", () => {
  assert.deepEqual(
    pensFromPlacements([
      { park_name: "CPT", operational_location_display: "Godel 2 - Part 1", animals: 38 },
    ]),
    [{ park: "CPT", pen: "Godel 2 - Part 1", animals: 38 }],
  );
  assert.deepEqual(pensFromPlacements(undefined), []);
});
