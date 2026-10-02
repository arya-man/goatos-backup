// Guard: kpi-long-figure. A figure of a lakh or more is compacted for the template widget (the icon /
// sparkline corner never covers it, 1440 and 390) and its exact value stays readable in the sub-line.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { compactFigure } from "./kpi-figure.ts";

test("kpi-long-figure: lakh / crore with the exact value in the sub-line", () => {
  // Money: whole rupees, ₹ attached to the exact value (never "₹ lakh · 2,84,663.25").
  assert.deepEqual(compactFigure(284663.25, "₹"), { total: 2.85, caption: "₹ lakh · ₹2,84,663" });
  assert.deepEqual(compactFigure(-284663.25, "₹ · 12 loads costed"), { total: -2.85, caption: "₹ lakh · -₹2,84,663 · 12 loads costed" });
  assert.deepEqual(compactFigure(123456789, "₹"), { total: 12.35, caption: "₹ crore · ₹12,34,56,789" });
  assert.deepEqual(compactFigure(8993623.4, "₹ · Spent this year"), { total: 89.94, caption: "₹ lakh · ₹89,93,623 · Spent this year" });
  // A measured unit stays with its figure (never "lakh · 2,19,305 · kg").
  assert.deepEqual(compactFigure(219305, "kg · on the issued sheet"), { total: 2.19, caption: "lakh kg · 2,19,305 kg · on the issued sheet" });
  assert.deepEqual(compactFigure(150000, "in the herd now"), { total: 1.5, caption: "lakh · 1,50,000 · in the herd now" });
  assert.deepEqual(compactFigure(99999, "₹"), { total: 99999, caption: "₹" });
  assert.deepEqual(compactFigure(null, "—"), { total: null, caption: "—" });
});

test("kpi-long-figure: the adapter reads a compacted figure as figure + unit + exact sub-line", async () => {
  const { liftFigureUnit } = await import("./kpi-figure.ts");
  const money = compactFigure(8993623.4, "₹ · Spent this year");
  assert.deepEqual(liftFigureUnit(money.total, money.caption), { prefix: "₹", suffix: " lakh", caption: "₹89,93,623 · Spent this year", missing: false });
  const kg = compactFigure(219305, "kg · on the issued sheet");
  assert.deepEqual(liftFigureUnit(kg.total, kg.caption), { prefix: "", suffix: " lakh kg", caption: "2,19,305 kg · on the issued sheet", missing: false });
});

test("kpi-long-figure: the adapter compacts every figure it hands a template widget", () => {
  const adapter = readFileSync(new URL("../components/app/kpi-widget.tsx", import.meta.url), "utf8");
  assert.match(adapter, /compactFigure\(rawTotal, rawCaption\)/);
});

test("kpi-unit-with-figure: the unit lead joins the figure, a missing figure reads —", async () => {
  const { liftFigureUnit } = await import("./kpi-figure.ts");
  assert.deepEqual(liftFigureUnit(80, "% · Packing + distribution approved by the verifier"), { prefix: "", suffix: "%", caption: "Packing + distribution approved by the verifier", missing: false });
  assert.deepEqual(liftFigureUnit(23.77, "₹ lakh · 23,76,908 · Rolling 92 days"), { prefix: "₹", suffix: " lakh", caption: "23,76,908 · Rolling 92 days", missing: false });
  assert.deepEqual(liftFigureUnit(75341, "₹ · Last 7 days"), { prefix: "₹", suffix: "", caption: "Last 7 days", missing: false });
  assert.deepEqual(liftFigureUnit(null, "— · kg on the issued sheet"), { prefix: "", suffix: "", caption: "kg on the issued sheet", missing: true });
  assert.deepEqual(liftFigureUnit(null, "— · ₹ per animal per day"), { prefix: "", suffix: "", caption: "₹ per animal per day", missing: true });
  assert.deepEqual(liftFigureUnit(null, "Not enough closed cases yet"), { prefix: "", suffix: "", caption: "Not enough closed cases yet", missing: true });
  // Prose that merely starts like a unit is never lifted; a lone unit is lifted and leaves no sub-line.
  assert.deepEqual(liftFigureUnit(12, "kg on the issued sheet"), { prefix: "", suffix: "", caption: "kg on the issued sheet", missing: false });
  assert.deepEqual(liftFigureUnit(12, "of 30 deaths · last month"), { prefix: "", suffix: "", caption: "of 30 deaths · last month", missing: false });
  assert.deepEqual(liftFigureUnit(5, "kg · whole herd"), { prefix: "", suffix: " kg", caption: "whole herd", missing: false });
  assert.deepEqual(liftFigureUnit(75341, "₹"), { prefix: "₹", suffix: "", caption: undefined, missing: false });
});

test("kpi-unit-prop: KpiWidget's unit prop joins the caption convention", () => {
  const adapter = readFileSync(new URL("../components/app/kpi-widget.tsx", import.meta.url), "utf8");
  assert.match(adapter, /unit: unitProp/);
  assert.match(adapter, /const rawCaption = unitProp \? \[unitProp, givenCaption\]/);
});
