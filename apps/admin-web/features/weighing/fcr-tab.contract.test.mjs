// Source-shape contract tests for the FCR tab (model: growth-director.contract.test.mjs). They pin
// the wiring rules that keep the tab honest: it rides the page's single Promise.all under the same
// filters, its copy keys are all backend-authored, it renders no literal copy of its own, and it
// never divides two served figures into a ratio the backend did not publish.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pageSource = readFileSync(join(here, "weights-analytics.tsx"), "utf8");
// The tab list lives in the layout module the page and its loading.tsx share.
const layoutSource = readFileSync(join(here, "weights-analytics-layout.ts"), "utf8");
const tabSource = readFileSync(join(here, "fcr-tab.tsx"), "utf8");
const tableSource = readFileSync(join(here, "fcr-pens-table.tsx"), "utf8");
const loadSource = readFileSync(join(here, "load-comparison-tab.tsx"), "utf8");
const serviceSource = readFileSync(
  join(here, "..", "..", "..", "..", "backend", "internal", "adminui", "app", "service.go"),
  "utf8",
);

test("the FCR tab is in the strip and fetched inside the page's single Promise.all under the page filters", () => {
  assert.match(layoutSource, /export const WEIGHTS_TABS = \[[^\]]*"fcr"\]/, "WEIGHTS_TABS must end with the fcr tab");
  assert.match(pageSource, /const TABS = WEIGHTS_TABS;/);
  const promiseAll = pageSource.match(/const \[weights, growth, demographics, loadwise, loadValues, feedBand, fcr, salePrices\] = await Promise\.all\(\[[\s\S]*?\]\);/);
  assert.ok(promiseAll, "weights-analytics.tsx must keep a single Promise.all request plan");
  assert.match(
    promiseAll[0],
    /getWeighingFCR\(\{ \.\.\.scope, \.\.\.readWindow \}\)/,
    "the FCR read must carry the same park/period/sex/origin/weighing filters as the other reads",
  );
  assert.match(promiseAll[0], /getGrowthSalePrices\(\)/, "the Comparison tab's prices ride the same Promise.all");
  assert.match(pageSource, /firstAuthRequiredError\(weights, growth, demographics, loadwise, loadValues, feedBand, fcr, salePrices\)/);
});

test("every copy key the FCR tab uses is authored by the backend page contract", () => {
  const backendKeys = new Set([...serviceSource.matchAll(/"([a-z_]+(?:\.[a-z_]+)+)":/g)].map((m) => m[1]));
  const used = new Set();
  for (const source of [tabSource, pageSource]) {
    for (const m of source.matchAll(/copy\(pageContract, "([^"$`]+)"/g)) used.add(m[1]);
    for (const m of source.matchAll(/copy\(pageContract, `([^`$]+)`\)/g)) used.add(m[1]);
  }
  // Keys composed from an id (`section.fcr.${id}.title`) expand over the five cuts.
  for (const id of ["breed", "sex", "band", "park", "origin"]) {
    used.add(`section.fcr.${id}.title`);
    used.add(`section.fcr.${id}.aria`);
  }
  const missing = [...used].filter((key) => key.startsWith("tab.fcr") || key.includes(".fcr") || key.startsWith("fcr."))
    .filter((key) => !backendKeys.has(key));
  assert.deepEqual(missing, [], "fcr keys used by the tab that service.go does not author");
});

test("the Comparison tab reads the sale price as data, never from page copy", () => {
  assert.doesNotMatch(loadSource, /load\.rate\./, "the hard-coded 430/450 copy keys are retired");
  assert.doesNotMatch(serviceSource, /"load\.rate\.(sheep|goat)_per_kg"/, "service.go must not re-author a rate as copy");
  assert.match(loadSource, /salePrices: GrowthSalePrice\[\] \| null/, "prices arrive from the backend read");
});

test("the FCR tab computes no ratio of its own", () => {
  // The backend publishes fcr, feed_cost_per_kg_gain_inr and break_even_fcr. A division of two
  // served figures here would be a second number for the same fact.
  for (const source of [tabSource, tableSource]) {
    const withoutComments = source
      .replace(/\/\*[\s\S]*?\*\//g, "")
      .replace(/\/\/.*$/gm, "")
      // Import paths and copy keys carry slashes that are not arithmetic.
      .replace(/^import[\s\S]*?;$/gm, "")
      .replace(/"[^"\n]*"|`[^`]*`/g, '""');
    const divisions = [...withoutComments.matchAll(/[\w)\]]\s*\/\s*[\w(]/g)].map((m) => m[0]);
    assert.deepEqual(divisions, [], "no division between served figures in the FCR tab");
  }
});

test("the FCR tab has no literal visible copy", () => {
  for (const source of [tabSource]) {
    const withoutComments = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");
    const leaks = [];
    for (const m of withoutComments.matchAll(/>([^<>{}]*[A-Za-z]{3,}[^<>{}]*)</g)) {
      const text = m[1].trim();
      if (text && !/^(className|aria)/.test(text)) leaks.push(text);
    }
    assert.deepEqual(leaks, [], "JSX text nodes with words must come from copy()");
  }
});
