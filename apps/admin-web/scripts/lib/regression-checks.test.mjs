import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { REGRESSION_PATTERNS, assertRegressionPatterns, collectRegressionFindings } from "./regression-checks.mjs";

const source = readFileSync(new URL("./regression-checks.mjs", import.meta.url), "utf8");

test("exports the smoke entry point and the in-page collector", () => {
  assert.equal(typeof assertRegressionPatterns, "function");
  assert.equal(typeof collectRegressionFindings, "function");
});

test("every regression pattern family is declared and emitted by the collector", () => {
  for (const name of ["text-overlap", "text-cut-off", "A-chart-label-collapsed", "A-chart-label-clipped", "A-chart-label-overlap",
    "A-chart-label-column-narrow", "A-chart-labels-truncated", "A-chart-value-missing", "A-chart-empty-frame", "A-svg-text-clipped",
    "A-svg-text-overlap", "A-svg-text-tiny", "B-container-overflow", "C-cell-mid-word-wrap", "C-cell-overpaint", "chip-crushed",
    "J-raw-text", "D-page-overflow"]) {
    assert.ok(REGRESSION_PATTERNS[name], `declared: ${name}`);
    assert.ok(source.includes(`add("${name}"`), `emitted: ${name}`);
  }
  for (const name of Object.keys(REGRESSION_PATTERNS)) assert.ok(source.includes(`"${name}"`));
});

test("the pen-label family is declared here and emitted by the pen-label checker", async () => {
  // P patterns are raised in lib/pen-label-checks.mjs (judged in Node against the farm's
  // own pen list), not by the in-page collector, so they are asserted against that file.
  const penSource = readFileSync(new URL("./pen-label-checks.mjs", import.meta.url), "utf8");
  for (const name of ["P-pen-part-doubled", "P-pen-number-doubled", "P-pen-separator-wrong", "P-pen-whole-leaked", "P-pen-partition-missing"]) {
    assert.ok(REGRESSION_PATTERNS[name], `declared: ${name}`);
    assert.ok(penSource.includes(`"${name}"`), `emitted: ${name}`);
  }
  // The sweep must actually run them, at whatever viewport it is on.
  assert.match(source, /collectPenLabelIssues\(page\)/);
  assert.match(source, /penLabelFindings/);
});

test("covers the real chart class names and false-positive exclusions", () => {
  for (const cls of [".gcval", ".gclab", ".gcsub", ".mclab", ".mcv", ".wbl", ".wbl-text", ".wbv", ".hblab", ".hbval", "svg[role=img]", ".gcb:not(.gcempty)", ".celllink"]) assert.ok(source.includes(cls), cls);
  for (const guard of ["data-smoke-ignore", "aria-hidden", ".sr-only", "elementFromPoint", "rect\\(0"]) assert.ok(source.includes(guard), guard);
});

test("evidence is a viewport screenshot with red outlines and a short error", () => {
  assert.match(source, /outline:3px solid #e11d48/);
  assert.match(source, /-issues\.png/);
  assert.match(source, /fullPage: false/);
  assert.match(source, /screenshot_path=/);
  assert.match(source, /findings\.slice\(0, 3\)/);
});
