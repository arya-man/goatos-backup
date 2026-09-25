import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

// No whole-page reload and no flicker on the Sales pages (maintainer request 2026-09-25).
const read = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("no one-shape skeleton replaces every Sales page on every hop", () => {
  // sales/loading.tsx was ONE fixed skeleton for nine different pages: every chip, pager and
  // sidebar hop flashed the wrong shape. Without it a soft navigation keeps the current page on
  // screen until the next one is ready.
  assert.ok(!existsSync(new URL("../../app/(admin)/sales/loading.tsx", import.meta.url)));
});

test("Market analytics question and city chips are view-only client state", () => {
  const trend = read("./market-trend-section.tsx");
  assert.match(trend, /^"use client";/);
  assert.doesNotMatch(trend, /<Link\b/, "a view-only pick must never navigate");
  assert.match(trend, /replaceLocalOverlayUrl\(/);
  assert.match(trend, /aria-pressed=\{q\.id === question\}/);
  const page = read("./market-analytics.tsx");
  assert.doesNotMatch(page, /hrefWithQuery\(sp, \{ question:/);
  assert.doesNotMatch(page, /hrefWithQuery\(sp, \{ city:/);
});

test("Farm born stages its filters and never dims the whole page", () => {
  const page = read("./sales-farm-born.tsx");
  assert.match(page, /<WorklistFilters[\s\S]*?deferApply[\s\S]*?holdChildren=\{false\}/);
  const bar = read("../../components/worklist-filters.tsx");
  assert.match(bar, /className=\{busy && holdChildren \? "wfbusy" : undefined\}/);
});

test("the Over 35 kg margin re-counts its own card in place", () => {
  const control = read("./sales-ready-tolerance-control.tsx");
  assert.doesNotMatch(control, /router\.replace|useRouter/, "Apply must not re-render the whole page");
  assert.match(control, /disabled=\{draftG === valueG \|\| pending\}/, "the pending state is shown, not thrown away");
  const card = read("./over35-kpi.tsx");
  assert.match(card, /await countOver35Action\(parkId, nextG\)/);
  assert.match(card, /replaceLocalOverlayUrl\(/);
  const actions = read("./over35-actions.ts");
  assert.match(actions, /^"use server";/);
  assert.doesNotMatch(actions, /revalidatePath/);
});

test("Summary and Farm value stop reading what their filter does not change", () => {
  const sold = read("./sales-sold.tsx");
  assert.doesNotMatch(sold, /getSalesOptions\(/, "the read-only Summary has no record form to feed");
  const value = read("./sales-farm-value.tsx");
  // The valuation starts BEFORE the assumptions round trip, and there is one weighing read.
  assert.ok(value.indexOf("const overviewPromise = getSalesOverview") < value.indexOf("await getGrowthAssumptions()"));
  assert.equal((value.match(/getShedWeights\(/g) ?? []).length, 1);
});

test("a pressed chip or pager says it is busy in place", () => {
  assert.match(read("./sales-chrome.tsx"), /<LinkPending \/>/);
  for (const file of ["./sales-sold.tsx", "./sales-buyer-analytics.tsx", "./sales-farm-born.tsx", "./sales-loads.tsx", "./market-analytics.tsx"]) {
    assert.match(read(file), /<LinkPending \/>/, file);
  }
});
