// guard: axis-label-keeps-head (PR #294 B3/E1/E2). A long category label is cut at its END, so the
// load number that leads "131 (CPT Castro 1, CPT Castro 2)" always survives on the axis, and the
// tooltip shows the whole label.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { AXIS_LABEL_MAX_CHARS, CATEGORY_AXIS_LABELS, formatCategoryLabel, fullCategoryTitle, truncateAxisLabel } from "./chart-axis-label.ts";

test("a long load label keeps its load number and loses its tail", () => {
  const label = "131 (CPT Castro 1, CPT Castro 2 +3)";
  const cut = truncateAxisLabel(label);
  assert.ok(cut.startsWith("131 (CPT"), cut);
  assert.ok(cut.endsWith("…"));
  assert.ok([...cut].length <= AXIS_LABEL_MAX_CHARS);
});

test("a short label and a non-string pass through untouched", () => {
  assert.equal(truncateAxisLabel("Castro 1"), "Castro 1");
  assert.equal(formatCategoryLabel(42), 42);
  assert.deepEqual(formatCategoryLabel(["Anantapur Sheep", "14 kids"]), ["Anantapur Sheep", "14 kids"]);
});

test("adversarial: a start-truncating helper would fail this", () => {
  const cut = truncateAxisLabel("130 · Green Fresh Farm (CBE Castro 2, CBE Yashoda 10)", 20);
  assert.match(cut, /^130 · /);
  assert.doesNotMatch(cut, /^…/);
});

test("the tooltip title is the full category at the hovered index", () => {
  const title = fullCategoryTitle(["131 (CPT Castro 1, CPT Castro 2 +3)", ["Sirohi", "14 kids"]]);
  assert.equal(title("131 (CPT…", { dataPointIndex: 0 }), "131 (CPT Castro 1, CPT Castro 2 +3)");
  assert.equal(title("x", { dataPointIndex: 1 }), "Sirohi · 14 kids");
  assert.equal(CATEGORY_AXIS_LABELS.formatter("131 (CPT Castro 1, CPT Castro 2 +3)").startsWith("131 (CPT"), true);
});

test("every shared category-chart wrapper applies it", () => {
  for (const file of ["./grouped-columns.tsx", "./app/trend-chart.tsx", "./app/bar-charts.tsx", "./app/column-chart-card.tsx", "./app/balance-statistics-card.tsx"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    assert.match(src, /labels: CATEGORY_AXIS_LABELS/, file);
  }
  // The template-backed cards have no tooltip title of their own, so they install the full one.
  for (const file of ["./app/column-chart-card.tsx", "./app/balance-statistics-card.tsx"]) {
    assert.match(readFileSync(new URL(file, import.meta.url), "utf8"), /fullCategoryTitle\(/, file);
  }
});
