// Guard: counts-breakdown-template-rows (TR1-#18). The /counts/breakdown pen rows are the template
// collapsible row (order-table-row): an expand IconButton with the rotating arrow, text-line
// composition (primary + one secondary line capped with "+N"; TR2-P1-9 removed the Label cloud,
// guard breakdown-cells-no-chips), and the opened combinations as TableRows on the neutral
// ground with dashed dividers. No green expanded-row fill, no chip clouds, no legacy classes.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

const table = readFileSync(new URL("./counts-breakdown-pens-table.tsx", import.meta.url), "utf8");
const css = legacyCss("mesha-theme");

test("counts-breakdown-template-rows: template collapsible rows", () => {
  assert.match(table, /<IconButton[\s\S]{0,400}aria-expanded=\{isOpen\}/);
  assert.match(table, /eva:arrow-ios-downward-fill/);
  assert.doesNotMatch(table, /<Label\b/);
  assert.match(table, /COMPOSITION_SHOWN = 3/);
  assert.match(table, /<TableRow[\s\S]{0,300}bgcolor: "background\.neutral"/);
  for (const legacy of ["dimchip", "xtoggle", "xdetail", "xsplit", "agesplit", "xcount", "cb-xbar", "<tr", "<td"]) {
    assert.ok(!table.includes(legacy), `no legacy ${legacy}`);
  }
  assert.doesNotMatch(css, /tr\.xrow\.open td\{background:var\(--brand-soft\)/, "no green expanded-row fill");
});
