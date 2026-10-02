// guard: axis-label-keeps-head (PR #294 B3/E1/E2). A long category label is cut at its END, so the
// load number that leads "131 (CPT Castro 1, CPT Castro 2)" always survives on the axis, and the
// tooltip shows the whole label.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { AXIS_LABEL_MAX_CHARS, categoryAxisOptions, formatCategoryLabel, fullCategoryLabel, truncateAxisLabel } from "./chart-axis-label.ts";

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

test("category charts get the formatter and a full tooltip title; datetime, numeric and own formatters do not", () => {
  const opts = categoryAxisOptions({ xaxis: { categories: ["a"] } });
  assert.equal(typeof opts.xaxis.labels.formatter, "function");
  assert.equal(opts.tooltip.x.formatter("131 (CPT Castro 1, CPT Castro 2 +3)"), "131 (CPT Castro 1, CPT Castro 2 +3)");
  assert.equal(fullCategoryLabel(["Sirohi", "14 kids"]), "Sirohi · 14 kids");
  assert.equal(categoryAxisOptions({ xaxis: { type: "datetime", categories: ["a"] } }), null);
  assert.equal(categoryAxisOptions({ xaxis: { type: "numeric", categories: [1] } }), null);
  assert.equal(categoryAxisOptions({ xaxis: { categories: ["a"], labels: { formatter: () => "" } } }), null);
  assert.equal(categoryAxisOptions({ xaxis: {} }), null);
  // A caller's own tooltip title wins.
  assert.equal(categoryAxisOptions({ xaxis: { categories: ["a"] }, tooltip: { x: { formatter: () => "" } } }).tooltip, undefined);
});

test("useChart installs it for every chart", () => {
  const src = readFileSync(new URL("./minimal/chart/use-chart.ts", import.meta.url), "utf8");
  assert.match(src, /categoryAxisOptions\(/);
});
