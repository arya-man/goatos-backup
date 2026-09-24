import assert from "node:assert/strict";
import { test } from "node:test";
import {
  chartAccessibleLabel,
  chartLayout,
  isRenderableChart,
} from "./ceo-ai-chart-geometry.ts";

const byPark = {
  type: "bar",
  title: "Vaccination overdue",
  x: ["Castro 1", "Gandhi 2", "Nehru 3"],
  series: [{ name: "Vaccination overdue", data: [12, 7, 3] }],
};

test("renders a bar chart whose bars match the tool rows", () => {
  assert.equal(isRenderableChart(byPark), true);
  const layout = chartLayout(byPark);
  assert.ok(layout && layout.kind === "bar");
  assert.equal(layout.bars.length, 3);
  assert.deepEqual(
    layout.bars.map((b) => b.value),
    [12, 7, 3],
  );
  // Bar widths are proportional: the max value (12) is the widest bar.
  const widest = Math.max(...layout.bars.map((b) => b.pct));
  assert.equal(layout.bars[0].pct, widest);
});

test("renders a line chart for a trend series with one point per x label", () => {
  const trend = {
    type: "line",
    title: "Overdue trend",
    x: ["Jan", "Feb", "Mar"],
    series: [{ name: "Overdue", data: [5, 9, 4] }],
  };
  const layout = chartLayout(trend);
  assert.ok(layout && layout.kind === "line");
  assert.equal(layout.points.length, 3);
  assert.deepEqual(
    layout.points.map((p) => p.value),
    [5, 9, 4],
  );
  // The peak (9, Feb) sits highest => smallest y.
  const minY = Math.min(...layout.points.map((p) => p.cy));
  assert.equal(layout.points[1].cy, minY);
  assert.ok(layout.path.startsWith("M"));
});

test("refuses to render a single-point (plain count) chart", () => {
  const single = { type: "bar", title: "x", x: ["Castro 1"], series: [{ name: "x", data: [22] }] };
  assert.equal(isRenderableChart(single), false);
  assert.equal(chartLayout(single), null);
});

test("refuses non-finite or malformed charts", () => {
  assert.equal(isRenderableChart(undefined), false);
  assert.equal(isRenderableChart({ type: "pie", x: ["a", "b"], series: [{ name: "s", data: [1, 2] }] }), false);
  assert.equal(
    isRenderableChart({ type: "bar", x: ["a", "b"], series: [{ name: "s", data: [1, NaN] }] }),
    false,
  );
});

test("accessible label enumerates real values", () => {
  const label = chartAccessibleLabel(byPark);
  assert.match(label, /Vaccination overdue/);
  assert.match(label, /Castro 1: 12/);
  assert.match(label, /Gandhi 2: 7/);
});

test("long category labels are kept whole so distinct bars never look identical", () => {
  const long = {
    type: "bar",
    title: "Revenue by customer",
    x: [
      "Mesha Kids Concept Store Orchard Road",
      "Mesha Kids Concept Store Jurong East",
      "A",
    ],
    series: [{ name: "Revenue", data: [1200.456, 800, 0] }],
  };
  const layout = chartLayout(long);
  assert.ok(layout && layout.kind === "bar");
  const labels = layout.bars.map((b) => b.label);
  assert.deepEqual(labels, long.x);
  assert.equal(new Set(labels).size, 3);
  assert.ok(labels.every((l) => !l.includes("…")));
  // Proportional percentages; zero still shows a sliver; max is 100%.
  assert.equal(layout.bars[0].pct, 100);
  assert.ok(Math.abs(layout.bars[1].pct - 66.6) < 0.2);
  assert.equal(layout.bars[2].pct, 1);
  assert.equal(layout.bars[0].valueLabel, "1,200");
});

test("line ticks are thinned for many points and keep first and last", () => {
  const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  const layout = chartLayout({
    type: "line",
    title: "t",
    x: months,
    series: [{ name: "s", data: months.map((_, i) => i + 1) }],
  });
  assert.ok(layout && layout.kind === "line");
  assert.ok(layout.ticks.length <= 5);
  assert.equal(layout.ticks[0], 0);
  assert.equal(layout.ticks.at(-1), 11);
});

test("long line labels get fewer ticks", () => {
  const x = Array.from({ length: 12 }, (_, i) => `W${27 + i} (0${i % 9 + 1}/07)`);
  const layout = chartLayout({ type: "line", title: "t", x, series: [{ name: "a", data: x.map((_, i) => i + 1) }] });
  assert.deepEqual(layout.ticks, [0, 6, 11]);
});

test("draws every series, with a legend, when the chart compares several", () => {
  const layout = chartLayout({
    type: "line",
    title: "Castro 1 weekly gain, Coimbatore vs Channapatna",
    x: ["27/07", "03/08", "10/08", "17/08"],
    series: [
      { name: "Coimbatore", data: [195, 337, 60, 212] },
      { name: "Channapatna", data: [120, 133, 91, 280] },
    ],
  });
  assert.ok(layout && layout.kind === "line");
  assert.equal(layout.lines.length, 2);
  assert.deepEqual(layout.legend.map((l) => l.name), ["Coimbatore", "Channapatna"]);
  assert.notEqual(layout.lines[0].color, layout.lines[1].color);
  assert.deepEqual(layout.lines[1].points.map((p) => p.value), [120, 133, 91, 280]);
  const label = chartAccessibleLabel({ type: "line", title: "t", x: ["a", "b"], series: [{ name: "A", data: [1, 2] }, { name: "B", data: [3, 4] }] });
  assert.match(label, /A: a 1, b 2; B: a 3, b 4/);
});

test("a single series draws one line and no legend", () => {
  const layout = chartLayout({ type: "line", title: "t", x: ["a", "b", "c"], series: [{ name: "s", data: [1, 5, 3] }] });
  assert.equal(layout.lines.length, 1);
  assert.equal(layout.legend.length, 0);
});

test("y-axis labels span the values, and weights are not squashed onto a zero baseline", () => {
  const layout = chartLayout({ type: "line", title: "kg", x: ["03/08", "10/08", "17/08", "21/09"], series: [{ name: "kg", data: [30.8, 31.3, 32.5, 39.1] }] });
  const values = layout.yTicks.map((t) => t.value);
  assert.ok(Math.min(...values) <= 30.8 && Math.max(...values) >= 39.1);
  assert.ok(Math.min(...values) > 0, "a 30-39 kg band does not start the axis at 0");
  assert.ok(layout.yTicks.every((t) => t.label.length > 0));
  // Lowest value sits lowest (largest y), highest sits highest.
  const ys = layout.points.map((p) => p.cy);
  assert.equal(Math.max(...ys), layout.points[0].cy);
  assert.equal(Math.min(...ys), layout.points[3].cy);
});

test("a negative value is drawn below zero, not clamped to it, with a zero line", () => {
  const layout = chartLayout({ type: "line", title: "g/day", x: ["a", "b", "c"], series: [{ name: "g", data: [136, -476, 212] }] });
  assert.ok(layout.zeroY !== null);
  assert.ok(layout.points[1].cy > layout.zeroY, "-476 sits below the zero line");
  assert.ok(layout.yTicks.some((t) => t.value < 0));
});

test("bars compare several series per row", () => {
  const layout = chartLayout({ type: "bar", title: "t", x: ["Castro 1", "Castro 2"], series: [{ name: "CBE", data: [39.1, 34.2] }, { name: "CPT", data: [31.4, 0] }] });
  assert.equal(layout.bars[0].parts.length, 2);
  assert.equal(layout.bars[0].parts[1].name, "CPT");
  assert.equal(layout.bars[1].parts[1].pct, 1);
  assert.deepEqual(layout.legend.map((l) => l.name), ["CBE", "CPT"]);
});

test("missing readings (null) are gaps, never zero", () => {
  const bar = chartLayout({
    type: "bar", title: "Pen gain, Coimbatore vs Channapatna", x: ["P1", "P2", "P3"],
    series: [{ name: "Coimbatore", data: [10, 12, 8] }, { name: "Channapatna", data: [9, null, 7] }],
  });
  const p2 = bar.bars[1].parts[1];
  assert.equal(p2.value, null);
  assert.equal(p2.pct, 0);
  assert.equal(p2.valueLabel, "–");
  const line = chartLayout({ type: "line", title: "t", x: ["a", "b", "c", "d"], series: [{ name: "s", data: [1, 2, null, 4] }] });
  assert.equal(line.points[2].cy, null);
  assert.equal((line.path.match(/M/g) || []).length, 2, "line breaks at the gap");
  assert.ok(!line.yTicks.some((t) => t.value === 0) || Math.min(...line.yTicks.map((t) => t.value)) === 0);
  assert.match(chartAccessibleLabel({ type: "bar", title: "t", x: ["a", "b", "c"], series: [{ name: "s", data: [1, null, 3] }] }), /b: no data/);
});

test("a series needs two real readings; NaN still rejects", () => {
  assert.equal(isRenderableChart({ type: "bar", title: "t", x: ["a", "b"], series: [{ name: "s", data: [1, null] }] }), false);
  assert.equal(isRenderableChart({ type: "bar", title: "t", x: ["a", "b", "c"], series: [{ name: "s", data: [1, null, 2] }] }), true);
  assert.equal(isRenderableChart({ type: "bar", title: "t", x: ["a", "b"], series: [{ name: "s", data: [1, "2"] }] }), false);
});

test("a single-series bar chart uses one colour for every bar", () => {
  const layout = chartLayout({ type: "bar", title: "Weekly gain", x: ["W1", "W2", "W3", "W4"], series: [{ name: "g", data: [1, 2, 3, 4] }] });
  assert.equal(new Set(layout.bars.map((b) => b.color)).size, 1);
});

test("series are capped at the palette size so colours never repeat", () => {
  const series = Array.from({ length: 9 }, (_, i) => ({ name: `S${i}`, data: [i, i + 1] }));
  const layout = chartLayout({ type: "line", title: "t", x: ["a", "b"], series });
  assert.equal(layout.lines.length, 7);
  assert.equal(new Set(layout.lines.map((l) => l.color)).size, 7);
});
