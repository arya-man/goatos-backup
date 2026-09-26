import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { fmtDate } from "../lib/format.ts";

const source = readFileSync(new URL("./worklist-filters.tsx", import.meta.url), "utf8");

// Every VISIBLE date is DD/MM/YYYY (maintainer lock 2026-09-10); ISO stays on the wire. The applied-
// filter chips are built from the URL, whose date params ARE ISO business dates, and the date-range
// chip rendered them verbatim -- so /weighing/analytics showed "Period 2026-08-10 - 2026-09-22"
// directly beneath a picker reading "10/08/2026 to 22/09/2026". One filter, one screen, two formats.
//
// The guard scans for a screen hand-writing a date SHAPE; it cannot see a wire value reaching a text
// node through a variable, which is what this was. Hence a test at the point of use.
test("the applied date-range chip reads DD/MM/YYYY, not the ISO value it was built from", () => {
  assert.equal(fmtDate("2026-08-10"), "10/08/2026");
  assert.match(
    source,
    /value: `\$\{fmtDate\(from \?\? field\.defaultFrom\)\} – \$\{fmtDate\(to \?\? field\.defaultTo\)\}`/,
    "both ends of the range chip go through fmtDate",
  );
});
