// Guard: kpi-long-figure. A figure of a lakh or more is compacted for the template widget (the icon /
// sparkline corner never covers it, 1440 and 390) and its exact value stays readable in the sub-line.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { compactFigure } from "./kpi-figure.ts";

test("kpi-long-figure: lakh / crore with the exact value in the sub-line", () => {
  assert.deepEqual(compactFigure(284663.25, "₹"), { total: 2.85, caption: "₹ lakh · 2,84,663.25" });
  assert.deepEqual(compactFigure(-284663.25, "₹ · 12 loads costed"), { total: -2.85, caption: "₹ lakh · -2,84,663.25 · 12 loads costed" });
  assert.deepEqual(compactFigure(123456789, "₹"), { total: 12.35, caption: "₹ crore · 12,34,56,789" });
  assert.deepEqual(compactFigure(150000, "in the herd now"), { total: 1.5, caption: "lakh · 1,50,000 · in the herd now" });
  assert.deepEqual(compactFigure(99999, "₹"), { total: 99999, caption: "₹" });
  assert.deepEqual(compactFigure(null, "—"), { total: null, caption: "—" });
});

test("kpi-long-figure: the adapter compacts every figure it hands a template widget", () => {
  const adapter = readFileSync(new URL("../components/app/kpi-widget.tsx", import.meta.url), "utf8");
  assert.match(adapter, /compactFigure\(rawTotal, rawCaption\)/);
});
