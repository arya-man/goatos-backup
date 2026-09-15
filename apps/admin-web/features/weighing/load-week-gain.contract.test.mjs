// Source-shape contract tests for the Time-wise tab's per-load weekly table (maintainer request
// 2026-09-14, "Time-wise ADG for each shed/load"; model: pen-week-gain.contract.test.mjs). They pin
// the wiring rules that keep the grid honest: it reads the backend's `gain_by_load_week` off the
// SAME demographics response the pen and breed rows use (so the three can never describe
// different weeks), it renders the farm's own load number and supplier verbatim, it takes its
// fixed column labels from the `load-week-gain` contract, and it renders through the shared week
// pivot -- so a load-week the load was not weighed twice in shows the backend's blank marker, never
// a zero, by the same code path the pen grid is pinned on.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pageSource = readFileSync(join(here, "weights-analytics.tsx"), "utf8");
const tableSource = readFileSync(join(here, "load-week-gain-table.tsx"), "utf8");

test("the Time-wise tab reads gain_by_load_week off the demographics response", () => {
  assert.match(pageSource, /demo\?\.gain_by_load_week \?\? \[\]/, "the grid must read the backend's load x week series");
  assert.match(pageSource, /loadRef: point\.load_ref/, "the load number is the farm's own reference, rendered verbatim");
  assert.match(pageSource, /source: point\.owner_name/, "the supplier is the backend's owner name, rendered verbatim");
  assert.match(pageSource, /table\(pageContract, "load-week-gain"\)/, "fixed columns come from the load-week-gain contract");
  assert.match(pageSource, /tab === "time"[\s\S]*\? "weekly_gain"/, "weekly grids must be opt-in so unrelated demographics tabs stay light");
});

test("the load grid composes no copy of its own and renders through the shared pivot", () => {
  for (const key of ["section.time.load.title", "section.time.load.caption", "section.time.load.aria", "empty.time.load.body"]) {
    assert.ok(pageSource.includes(`"${key}"`), `page must read backend copy key ${key}`);
  }
  assert.doesNotMatch(tableSource, /copy\(/, "the table takes labels from the page, never the contract directly");
  assert.doesNotMatch(tableSource, />\s*(Load|Source|Week|No data|Supplier)\s*</, "no literal column or empty copy");
  assert.match(tableSource, /from "\.\/week-gain-table"/, "one pivot for pens and loads, so the two grids cannot drift");
  assert.doesNotMatch(tableSource, /\?\? 0\b/, "a missing gain must never be coerced to 0");
});

test("the load grid keys rows by load and source", () => {
  assert.ok(tableSource.includes("const rowKey = `${point.loadRef}\\u0000${point.source}`;"), "same load ref from two suppliers must not collapse");
  assert.match(tableSource, /seen\.has\(rowKey\)/, "row de-dupe must use the composite key");
  assert.ok(tableSource.includes("return { rowKey, weekStart:"), "cells must address the same composite row");
});
