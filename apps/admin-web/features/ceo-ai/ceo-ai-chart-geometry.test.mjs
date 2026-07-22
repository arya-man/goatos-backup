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
  const widest = Math.max(...layout.bars.map((b) => b.width));
  assert.equal(layout.bars[0].width, widest);
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
