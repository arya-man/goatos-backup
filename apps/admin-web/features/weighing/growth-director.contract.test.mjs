// Source-shape contract tests for the Growth Director block (model:
// features/people/vaccination-operators-screen.contract.test.mjs). These pin the
// wiring rules that keep the block safe on the Weights page: it joins the page's
// single Promise.all (no serial award waterfall), it respects the park/period
// filters, and it renders no literal visible copy of its own.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const weightsSource = readFileSync(join(here, "weights.tsx"), "utf8");
const sectionSource = readFileSync(join(here, "growth-director.tsx"), "utf8");

test("weights page fetches growth director inside the existing Promise.all", () => {
  const promiseAll = weightsSource.match(/Promise\.all\(\[[\s\S]*?\]\);/);
  assert.ok(promiseAll, "weights.tsx must keep a single Promise.all request plan");
  assert.match(
    promiseAll[0],
    /getGrowthDirector\(\{ park_id: parkFilter \|\| undefined, \.\.\.window \}\)/,
    "getGrowthDirector must ride the same park/period window as the other reads, inside Promise.all",
  );
});

test("growth director result is auth-gated with the other reads", () => {
  assert.match(
    weightsSource,
    /firstAuthRequiredError\(weights, growth, demographics, growthDirector\)/,
    "an auth-required growth-director response must redirect like the other reads",
  );
});

test("growth director section renders after the existing widgets", () => {
  const renderAt = weightsSource.indexOf("<GrowthDirectorSection");
  const losingAt = weightsSource.indexOf('"losing_kids.title"');
  assert.ok(renderAt > 0, "weights.tsx must render <GrowthDirectorSection>");
  if (losingAt > 0) {
    assert.ok(
      renderAt > losingAt,
      "the Growth Director block must come after the existing losing-kids card, never above the live dashboard",
    );
  }
});

test("growth director component has no literal visible copy", () => {
  // Every visible string must resolve through copy()/gd(). JSX text nodes that
  // contain letters (not interpolations, separators or entities) are the leak.
  const jsxTextLeaks = [];
  for (const match of sectionSource.matchAll(/>([^<>{}]+)</g)) {
    const text = match[1].trim();
    if (text && /[A-Za-z]{2,}/.test(text)) jsxTextLeaks.push(text);
  }
  assert.deepEqual(jsxTextLeaks, [], "visible copy must come from the page contract");
});

test("growth director component never renders an expected-count ratio", () => {
  // Weighing is free-flow: there is no expected roster, so no "x/expected" shape
  // may appear (guard mode 10). Denominators are worded, never slashed.
  assert.ok(
    !/expected/i.test(sectionSource),
    "no expected-count denominators — weighing has no roster",
  );
});
