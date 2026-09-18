import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./svg-column-bars.tsx", import.meta.url), "utf8");

test("column bars use the empty state for all-zero data instead of blank mobile chart scaffolding", () => {
  assert.match(source, /const hasAnyValue = data\.some\(\(d\) => d\.value > 0 \|\| \(d\.compareValue \?\? 0\) > 0\);/);
  assert.match(source, /data\.length === 0 \|\| !hasAnyValue/);
});

test("zero values do not receive the minimum visible bar height", () => {
  assert.match(source, /datum\.value > 0 \? 2 : 0/);
  assert.match(source, /compare > 0 \? 2 : 0/);
  assert.doesNotMatch(source, /Math\.max\(\(datum\.value \/ max\) \* plotHeight,\s*2\)/);
  assert.doesNotMatch(source, /Math\.max\(\(compare \/ max\) \* plotHeight,\s*2\)/);
});
