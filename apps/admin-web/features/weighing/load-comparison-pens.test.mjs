import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./load-comparison-tab.tsx", import.meta.url), "utf8");
const contract = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);

test("each load on the comparison chart names the pens its weighed animals sit in", () => {
  // A bar saying a supplier's stock grew 1.4x, with no pen named, cannot be walked from: the
  // reader has no way to reach the animals it describes. The pens ride the group's own sub-line,
  // beside the multiple, so they are VISIBLE rather than hidden behind a hover panel.
  assert.match(source, /pens: penList\(bucket\)/);
  assert.match(source, /subheading: \[multiple, row\.pens\]\.filter\(Boolean\)\.join\(" · "\) \|\| undefined/);
  // Head count comes from the same placement row the load's own average is weighted by, so the
  // line and the chart cannot disagree about how many animals were weighed where.
  assert.match(source, /\$\{where\} · \$\{p\.animals\.toLocaleString\("en-IN"\)\}/);
});

test("the pen line takes its names from the backend, and disambiguates parks only when it must", () => {
  // The display string is BACKEND-composed. This file must never join a shed name to a partition
  // label itself -- that is the hand-rolled composition the operational-location rule bans.
  assert.match(source, /p\.operational_location_display/);
  assert.doesNotMatch(source, /partition_label/);
  // A shed name repeats across parks (a "Castro 1" in each), so a load split across both prefixes
  // the park; a single-park load does not, or the prefix would push the pens off the line.
  assert.match(source, /const multiPark = new Set\(placements\.map\(\(p\) => p\.park_name\)\.filter\(Boolean\)\)\.size > 1;/);
  // No pens is an ABSENT sub-line, never a dangling separator beside the multiple.
  assert.match(source, /if \(placements\.length === 0\) return "";/);
});

test("the caption says the pens are there", () => {
  assert.match(contract, /followed by the pens the load's weighed animals sit in/);
});
