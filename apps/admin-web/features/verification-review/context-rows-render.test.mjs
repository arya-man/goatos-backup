// The verifier judges a proof against what the work was CLAIMED to be, and the backend states
// that claim in `context_rows` — producer-attached label/value pairs (e.g. the milk litres and
// citric acid grams the operator entered with a milk preparation submission, or a feed packing
// item's frozen ration). Android's verify detail has always rendered them; the web drawer did
// not, so a web verifier could confirm the videos existed but never see the entered quantities.
//
// Source-shape tests, like every other test in this folder: there is no DOM harness here, and
// the defect was a missing render of an already-served wire field.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");

test("the drawer renders backend context_rows verbatim in the facts grid", () => {
  assert.match(
    drawerSource,
    /item\.context_rows\s*\?\?\s*\[\]/,
    "the drawer must read item.context_rows (tolerating an absent field from an older backend)",
  );
  assert.match(
    drawerSource,
    /context_rows[\s\S]{0,400}className="vr-fact"[\s\S]{0,200}\{row\.label\}[\s\S]{0,200}\{row\.value\}/,
    "each context row must render as a vr-fact with the backend's own label and value, verbatim",
  );
});

test("a context row missing either half states nothing", () => {
  assert.match(
    drawerSource,
    /row\.label\?\.trim\(\)\s*&&\s*row\.value\?\.trim\(\)/,
    "a label with no value (or the reverse) must be dropped, matching the Android verify detail",
  );
});

test("a context row group starts a titled section when it changes", () => {
  assert.match(
    drawerSource,
    /function contextRowGroupStart[\s\S]{0,300}rows\[index - 1\]\?\.group/,
    "the drawer must compare each row's group with the previous row's",
  );
  assert.match(
    drawerSource,
    /context_rows[\s\S]{0,300}contextRowGroupStart\(rows, index\)[\s\S]{0,40}className="vr-fact-group" sx=\{FACT_GROUP_SX\}>\{contextRowGroupStart\(rows, index\)\}/,
    "a changed group renders the backend's own header verbatim in a vr-fact-group block before the row",
  );
});

// A step that asks one question groups its answer under the step title, which is often the question
// itself: the drawer read "WAS THE GATE LATCHED?" as a header and again as the row label. A header
// equal to the row's own label says nothing, so it is not rendered.
test("a group header equal to the row's own label is not repeated", () => {
  const match = drawerSource.match(/export function contextRowGroupStart\(([^)]*)\)[^{]*\{([\s\S]*?)\n\}/);
  assert.ok(match, "the drawer must still define contextRowGroupStart");
  // eslint-disable-next-line no-new-func
  const start = new Function("rows", "index", match[2]);
  const rows = [
    { label: "Pen", value: "Castro 2" },
    { label: "Was the gate latched?", value: "Yes", group: " was the gate latched? " },
    { label: "Trough clean?", value: "Yes", group: "Crew answers" },
    { label: "Water fresh?", value: "No", group: "Crew answers" },
  ];
  assert.deepEqual(rows.map((_, i) => start(rows, i)), ["", "", "Crew answers", ""]);
});
