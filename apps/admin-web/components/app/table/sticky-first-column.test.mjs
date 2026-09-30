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

// guard: sticky-edges-phone (FIXJ11, J3B N-P1-1). c4bd347ff deleted app/minimal-theme.css and with it
// the phone rule that pinned the first (identity) and last (action) cell of every `.tablewrap`
// table; no guard noticed. The rule is theme-token CSS-in-JS in AppBaseline now, for every table in
// the page content column (PagedRows, DataTable and raw tables alike). Runtime half: r2 plugin table-scroll
// (`sticky-identity`, 390 dark, every route).
const edges = sx;
test("guard: sticky-edges-phone - pinned first + last cells below 640px, zero specificity, colspan skipped", () => {
  assert.match(edges, /PHONE_STICKY_EDGES_QUERY = "@media \(max-width: 640px\)"/);
  assert.match(edges, /:where\(\$\{table\}:not\(\[data-sticky-edges="off"\]\)\$\{edge === "last" \? actions : ""\} > \$\{part\} > tr > :\$\{edge\}-child:not\(\[colspan\]\)\)/);
  assert.match(edges, /position: "sticky", left: 0, zIndex: 2, backgroundColor: paper/);
  assert.match(edges, /position: "sticky", right: 0, zIndex: 2, backgroundColor: paper/);
  // the last column pins only as the row's action column (a data column there covered the scroller)
  assert.match(edges, /const actions = `:has\(> tbody > tr > :last-child \$\{ROW_ACTION\}, > tbody > tr > :last-child > \.MuiIconButton-root\)`;/);
  assert.match(edges, /\$\{edge === "last" \? actions : ""\}/);
  assert.match(edges, /zIndex: 3, backgroundColor: neutral/);
});

test("guard: sticky-edges-phone - one AppBaseline rule covers every page-content table (adapters included)", () => {
  const baseline = read("theme/app-baseline.tsx");
  assert.match(baseline, /\[PHONE_STICKY_EDGES_QUERY\]: \{\s*\.\.\.phoneStickyEdgeCells\(`\$\{CONTENT\} table`, theme\)/, "AppBaseline covers every content table");
  assert.equal((baseline.match(/'@media \(max-width: 640px\)'/g) ?? []).length, 0, "one 640px block (a duplicate key would drop the other)");
  // Never as component sx: emotion prefixes the class onto `:where(&…)` and the rule never matches.
  for (const f of ["components/app/paged-rows.tsx", "components/data-table.tsx", "components/dense-table.tsx"]) {
    assert.doesNotMatch(read(f), /phoneStickyEdgeCells|PHONE_STICKY_EDGES/, f);
  }
});
