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

test("covers the real chart class names and false-positive exclusions", () => {
  for (const cls of [".gcval", ".gclab", ".gcsub", ".mclab", ".mcv", ".wbl", ".wbl-text", ".wbv", ".hblab", ".hbval", "svg[role=img]", ".gcb:not(.gcempty)", ".celllink"]) assert.ok(source.includes(cls), cls);
  for (const guard of ["data-smoke-ignore", "aria-hidden", ".sr-only", "elementFromPoint", "rect\\(0"]) assert.ok(source.includes(guard), guard);
  // A chart card drawn as a grid (visible table rows) is not an empty frame.
  assert.match(source, /tableRows\.length === 0/);
});

test("evidence is a viewport screenshot with red outlines and a short error", () => {
  assert.match(source, /outline:3px solid #e11d48/);
  assert.match(source, /-issues\.png/);
  assert.match(source, /fullPage: false/);
  assert.match(source, /screenshot_path=/);
  assert.match(source, /findings\.slice\(0, 3\)/);
});
