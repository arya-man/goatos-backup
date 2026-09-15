// Source-shape contract tests for the Time-wise tab's per-pen weekly table (maintainer request
// 2026-09-08; model: fcr-tab.contract.test.mjs). They pin the wiring rules that keep the table
// honest: it reads the backend's `gain_by_pen_week` (the same demographics read the breed rows
// use, so the two can never describe different weeks), it renders the backend-composed pen label
// verbatim, it takes its fixed column labels from the `pen-week-gain` contract, and a week the pen
// was not weighed twice renders the backend's blank marker rather than a zero.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pageSource = readFileSync(join(here, "weights-analytics.tsx"), "utf8");
const tableSource = readFileSync(join(here, "pen-week-gain-table.tsx"), "utf8");
// The pivot itself (cells, week columns, blank marker) lives in the shared table both the pen and
// the load grids render through; the wrapper only names its rows.
const pivotSource = readFileSync(join(here, "week-gain-table.tsx"), "utf8");

test("the Time-wise tab reads gain_by_pen_week off the demographics response", () => {
  assert.match(pageSource, /demo\?\.gain_by_pen_week \?\? \[\]/, "the grid must read the backend's pen x week series");
  assert.match(
    pageSource,
    /pen: point\.operational_location_display \|\| point\.shed_name/,
    "the pen label is the backend-composed operational location, rendered verbatim",
  );
  assert.match(pageSource, /table\(pageContract, "pen-week-gain"\)/, "fixed columns come from the pen-week-gain contract");
});

test("the table composes no copy of its own", () => {
  for (const key of [
    "section.time.pen.title",
    "section.time.pen.caption",
    "section.time.pen.aria",
    "empty.time.pen.body",
    "value.time.pen.blank",
    "value.time.pen.unit",
  ]) {
    assert.ok(pageSource.includes(`"${key}"`), `page must read backend copy key ${key}`);
  }
  // The component takes labels and a contract; the only literal strings it renders are the unit
  // suffix on a measured number and the ISO week date, which is data.
  for (const source of [tableSource, pivotSource]) {
    assert.doesNotMatch(source, /copy\(/, "the table takes labels from the page, never the contract directly");
    assert.doesNotMatch(source, />\s*(Pen|Park|Week|No data|Shed|Load|Source)\s*</, "no literal column or empty copy");
  }
});

test("a week without a second weigh renders the blank marker, never zero", () => {
  assert.match(pivotSource, /<span className="muted">\{labels\.blank\}<\/span>/, "absence renders the backend's blank marker");
  assert.doesNotMatch(pivotSource, /\?\? 0\b/, "a missing gain must never be coerced to 0");
  assert.doesNotMatch(tableSource, /\?\? 0\b/, "a missing gain must never be coerced to 0");
  assert.match(pivotSource, /\[\.\.\.new Set\(cells\.map\(\(cell\) => cell\.weekStart\)\)\]\.sort\(\)/, "week columns are the served weeks, ascending");
});

test("the pen grid keys its rows on the pen's identity, never its name", () => {
  assert.match(tableSource, /`\$\{point\.locationId\}::\$\{point\.partitionLabel\}`/, "two parks can hold a pen of the same name");
});
