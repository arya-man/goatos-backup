import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const source = readFileSync(new URL("./sales-executive-analytics.tsx", import.meta.url), "utf8");

test("sales executive analytics lets backend refuse invalid query params", () => {
  assert.match(source, /function queryInt\(/);
  assert.ok(source.includes("if (!/^-?\\d+$/.test(trimmed)) return Number.NaN;"));
  assert.doesNotMatch(source, /boundedInt\(/);
  assert.doesNotMatch(source, /FALLBACK_PERIODS as readonly number\[\]\)\.includes/);
  assert.match(source, /getSalesExecutiveAnalytics\(\{\s*days,\s*activityOffset,\s*vendorOffset,/s);
});
