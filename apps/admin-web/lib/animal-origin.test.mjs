import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// The helper is TypeScript; this test reads it as source, the pattern the other lib tests use, and
// pins the two properties the three-way origin filter depends on.
const src = readFileSync(new URL("./animal-origin.ts", import.meta.url), "utf8");

test("the origin filter offers exactly the three cohorts, farm born first", () => {
  assert.match(src, /ORIGIN_KEYS = \["farm_born", "procured_no_load", "procured_load"\] as const/);
});

test("a bookmark from the two-way filter keeps its meaning: purchased is the load cohort", () => {
  assert.match(src, /if \(value === "purchased"\) return "procured_load";/);
});

test("an old origin=purchased link is rewritten to procured_load, keeping every other filter", () => {
  assert.match(src, /if \(value !== "purchased"\) return null;/);
  assert.match(src, /next\.set\("origin", "procured_load"\);/);
});
