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
