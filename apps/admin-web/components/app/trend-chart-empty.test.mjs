// guard: chart-empty-frame (PR #294 C1). A TrendChart whose window holds no figure (every value
// null or 0) renders the empty state, never a bare axis frame with no marks.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const src = readFileSync(new URL("./trend-chart.tsx", import.meta.url), "utf8");

test("an all-zero / all-null window is the empty state", () => {
  assert.match(src, /const allZero = series\.every\(\(s\) => magnitude\(s\.key\) === 0\);/);
  assert.match(src, /if \(data\.length === 0 \|\| series\.length === 0 \|\| allZero\) \{\s*return <EmptyState title=\{emptyLabel\} \/>;/);
  // The old bare 0..4 frame for an empty window is gone.
  assert.doesNotMatch(src, /\{ min: 0, max: 4, tickAmount: 4 \}/);
});
