import assert from "node:assert/strict";
import test from "node:test";

import { kpiTileSize } from "./kpi-grid-size.ts";

// guard: kpi-short-row-fills (FIXJ11): a data-sized deck never leaves dead slots in its last row.
test("kpi-short-row-fills: every breakpoint row of a 6+ deck sums to 12", () => {
  for (const n of [6, 7, 9, 10, 11, 13]) {
    for (const bp of ["sm", "md", "xl"]) {
      const sizes = Array.from({ length: n }, (_, i) => kpiTileSize(n, i)[bp]);
      let row = 0;
      for (const s of sizes) { row += s; if (row >= 12 - 1e-9) { assert.ok(Math.abs(row - 12) < 1e-9, `n=${n} ${bp} ${sizes}`); row = 0; } }
      assert.equal(row, 0, `n=${n} ${bp}: last row does not fill (${sizes})`);
    }
  }
  assert.deepEqual(kpiTileSize(7, 6), { xs: 12, sm: 12, md: 12, xl: 12 });
  assert.deepEqual(kpiTileSize(8, 7), { xs: 12, sm: 6, md: 3 }, "multiples of four stay rows of four");
  assert.deepEqual(kpiTileSize(3, 0), { xs: 12, sm: 4 });
});
