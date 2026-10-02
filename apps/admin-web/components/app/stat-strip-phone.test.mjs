import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// PR #294 L-C6: phone stat strips (template InvoiceAnalytic rows) cut their labels at 390 ("Accep",
// "Overd", "Pens in d") because the row panned sideways. Below sm the row is a two-column grid.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("stat strip cells are marked and laid out two to a row on a phone", () => {
  assert.match(read("./sections/invoice/invoice-analytic.tsx"), /data-stat-cell=""/);
  assert.match(read("./skeletons/blocks.tsx"), /data-stat-cell=""/, "the loading twin takes the same shape");
  const base = read("../../theme/app-baseline.tsx");
  assert.match(base, /'@media \(max-width: 599\.95px\)': \{\s*'\.MuiStack-root\.MuiStack-root:has\(> \[data-stat-cell\]\)': \{\s*display: 'grid',\s*gridTemplateColumns: 'repeat\(2, minmax\(0, 1fr\)\)'/);
  assert.match(base, /:has\(> \[data-stat-cell\]\) > \.MuiDivider-root': \{ display: 'none' \}/);
  // The sticky table edges keep their own phone query key (an object key collision would drop them).
  assert.equal((base.match(/\[theme\.breakpoints\.down\('sm'\)\]/g) || []).length, 0);
  assert.match(base, /\.\.\.phoneStickyEdges\(/);
});
