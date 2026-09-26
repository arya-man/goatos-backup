import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./grouped-columns.tsx", import.meta.url), "utf8");

test("grouped column missing series slots are hidden from mobile webview paint", () => {
  // A missing (null) series value reaches ApexCharts as null, which draws no bar at all -- never a
  // zero-height stub or a placeholder -- and prints no figure.
  assert.match(source, /data: data\.map\(\(d\) => d\.values\[i\]\)/);
  assert.doesNotMatch(source, /d\.values\[i\] \?\? 0/);
  assert.match(source, /datum\.values\[opts\.seriesIndex\] === null\) return ""/);
});
