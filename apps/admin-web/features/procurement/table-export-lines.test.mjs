import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: export-cell-lines (FJ3 P1-15): the toolbar Export CSV read each cell's textContent, which
// glues a two-line cell into one word ("Dry Masoor BhusaKaveri Agro Traders", "Delivered25/09/2026").
test("toolbar export reads each rendered line of a cell and joins them with a separator", () => {
  const src = readFileSync(new URL("./table-toolbar.tsx", import.meta.url), "utf8");
  assert.match(src, /\.map\(\(c\) => cell\(exportCellText\(c\)\)\)/);
  assert.match(src, /cell\.innerText/);
  assert.doesNotMatch(src, /cell\(\(c\.textContent/);
});
