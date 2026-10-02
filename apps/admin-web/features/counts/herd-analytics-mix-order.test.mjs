import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// /counts/analytics Breed mix and Pen tag mix read alphabetically (PR #294 O12); /counts/breakdown
// orders the same mixes by count. The page is a server component (MUI imports), so the ordering is
// pinned on its source: every toBars mix is count-ordered, ties keep the backend's order.
const source = readFileSync(new URL("./herd-analytics.tsx", import.meta.url), "utf8");

test("composition mixes are ordered biggest first, ties stable", () => {
  assert.match(source, /return byCount \? sortByCount\(bars\) : bars;/);
  assert.match(source, /toBars\(data\.breed, /);
  assert.doesNotMatch(source, /toBars\(data\.breed,[^)]*false\)/);
  assert.match(source, /toBars\(data\.age_band, ha\(pageContract, "label\.unassigned_stage"\), false\)/, "age bands keep their natural order");
  assert.match(source, /\.sort\(\(a, b\) => b\.bar\.value - a\.bar\.value \|\| a\.index - b\.index\)/);
});
