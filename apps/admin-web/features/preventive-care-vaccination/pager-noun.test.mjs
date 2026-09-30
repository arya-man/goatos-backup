import assert from "node:assert/strict";
import test from "node:test";

import { pagerNoun } from "./pager-noun.ts";

// guard: pager-noun-plural (FIXJ11, J3B P2-3): /workflows read "1–10 Workflowss".
test("pager-noun-plural: singular nouns get one s, served plurals stay as they are", () => {
  assert.equal(pagerNoun("row", 1), "row");
  assert.equal(pagerNoun("row", 25), "rows");
  assert.equal(pagerNoun("pen", 0), "pens");
  assert.equal(pagerNoun("Workflows", 40), "Workflows");
  assert.equal(pagerNoun("Workflows", 1), "Workflows");
});

// guard: pager-range-units (FIXJ11, J3B P2-4): the feed worklists page by PEN and draw a row per
// grain, so the range must count pens ("Rows: 10 · 1–40 rows" -> "1–10 rows"), the unit of the
// rows-per-page select and the offset.
test("pager-range-units: /feed/direction and /feed/packing count the pens on the page", async () => {
  const { readFileSync } = await import("node:fs");
  const read = (rel) => readFileSync(new URL(`../../${rel}`, import.meta.url), "utf8");
  const pager = read("components/worklist-pager.tsx");
  assert.match(pager, /const units = pageUnits \?\? rowCount;/);
  assert.match(pager, /const end = units === 0 \? 0 : offset \+ units;/);
  for (const f of ["features/feed/feed-direction.tsx", "features/feed/feed-packing.tsx"]) {
    assert.match(read(f), /pageUnits=\{new Set\(rows\.map\(\(row\) => row\.shed_id\)\)\.size\}/, f);
  }
});
