import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: sticky-first-column (FIXJ2 regression). The health Types, Diagnosis register and Treatment
// catalog tables keep their first column on screen while they scroll sideways (template TableCell
// sx: sticky, left 0, paper / neutral surface, above the cells). Legacy CSS used to do it.
const read = (rel) => readFileSync(new URL(`../../../${rel}`, import.meta.url), "utf8");
const sx = readFileSync(new URL("./sticky-first-column.ts", import.meta.url), "utf8");

test("guard: sticky-first-column - the shared sx pins the first cell on paper / neutral", () => {
  assert.match(sx, /"& tbody tr > :first-of-type": \{ position: "sticky", left: 0, zIndex: 2, bgcolor: "background\.paper" \}/);
  assert.match(sx, /"& thead tr > :first-of-type": \{ position: "sticky", left: 0, zIndex: 3, bgcolor: "background\.neutral" \}/);
});

test("guard: sticky-first-column - health tables use it", () => {
  assert.equal((read("features/health/health-types.tsx").match(/\.\.\.STICKY_FIRST_COLUMN_SX/g) ?? []).length, 2, "gaps + types");
  assert.match(read("features/health/health-register.tsx"), /\.\.\.STICKY_FIRST_COLUMN_SX/);
  assert.match(read("features/health/health-config.tsx"), /minWidth: 960, \.\.\.STICKY_FIRST_COLUMN_SX/);
});
