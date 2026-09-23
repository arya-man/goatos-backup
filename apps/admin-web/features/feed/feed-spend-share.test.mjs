// The Feed spend share pie shows EVERY ACTIVE FEED (maintainer request 2026-09-23): it has no
// feed list of its own and instead shares the per-item cards' hidden-feed rule, so UHT Milk --
// real money the animals drink, left off the chart by the retired bhusa + two-concentrate
// allowlist -- gets a slice, and a feed the farm starts buying appears with no edit. The retired
// goat/sheep split concentrates stay out of the pie and the cards alike.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const page = readFileSync(join(here, "feed-analytics.tsx"), "utf8");
const contract = readFileSync(join(here, "../../../../backend/internal/adminui/app/service.go"), "utf8");

// Re-evaluate the rule exactly as the page implements it, from its own source.
const fnSource = page.match(/function itemCardHidden\(rule: string, label: string\): boolean \{[\s\S]*?\n\}/)?.[0];
assert.ok(fnSource, "itemCardHidden must exist in the page");
const itemCardHidden = new Function(
  "rule",
  "label",
  fnSource.replace(/function itemCardHidden\(rule: string, label: string\): boolean/, "").replace(/^\s*\{/, "").replace(/\}\s*$/, ""),
);
const hidden = contract.match(/"chart\.item\.hidden_feeds":\s*"([^"]+)"/)?.[1];
assert.ok(hidden, "the backend contract must author the hidden-feed rule");

test("the pie carries no feed allowlist of its own any more", () => {
  assert.equal(
    contract.includes('"chart.spend_share.feeds"'),
    false,
    "the retired allowlist must not come back — a new feed would be missing from the pie until someone widened it",
  );
  assert.equal(page.includes("spendShareIncludes"), false, "the page must not keep an allowlist helper for the pie");
  assert.match(
    page,
    /money && money\.pricedDays > 0 && !itemCardHidden\(fa\(pageContract, "chart\.item\.hidden_feeds"\), series\.label\)/,
    "the pie must gate its slices on the cards' hidden-feed rule",
  );
});

test("every active feed gets a slice, UHT Milk included; the retired splits do not", () => {
  for (const label of ["Dry Masoor Bhusa", "Mesha Adult Concentrate", "Mesha Kids Concentrate", "UHT Milk", "uht milk"]) {
    assert.equal(itemCardHidden(hidden, label), false, `${label} must be a slice`);
  }
  for (const label of [
    "Mesha Adult Concentrate Goat",
    "Mesha Adult Concentrate Sheep",
    "Mesha Kids Goat Concentrate",
    "Mesha Kids Sheep Concentrate",
  ]) {
    assert.equal(itemCardHidden(hidden, label), true, `${label} must not be a slice`);
  }
});

test("a feed nobody has named is in, so the pie never goes stale on a new feed", () => {
  assert.equal(itemCardHidden(hidden, "Some Feed Bought Next Month"), false);
  assert.equal(itemCardHidden("", "anything"), false, "an empty rule hides nothing");
});

test("no two slices share a hue: a wrapped palette shade yields to the next unused colour", () => {
  const fnSource = page.match(/function distinctSliceColors\(slices: PieSlice\[\]\): PieSlice\[\] \{[\s\S]*?\n\}/)?.[0];
  assert.ok(fnSource, "distinctSliceColors must exist in the page");
  const body = fnSource
    .replace(/function distinctSliceColors\(slices: PieSlice\[\]\): PieSlice\[\]/, "")
    .replace(/^\s*\{/, "")
    .replace(/\}\s*$/, "")
    .replace(/: string\)/g, ")")
    .replace(/new Set<string>\(\)/, "new Set()");
  const FEED_SERIES_VARS = ["var(--brand)", "var(--info)", "var(--amber)", "var(--brand-d)"];
  const distinct = new Function("slices", "FEED_SERIES_VARS", body);
  const out = distinct(
    [
      { label: "Dry Masoor Bhusa", value: 3, colorVar: "var(--brand)" },
      { label: "Mesha Kids Concentrate", value: 2, colorVar: "var(--amber)" },
      { label: "Mesha Adult Concentrate", value: 1, colorVar: "var(--brand-d)" },
      { label: "Other", value: 1, colorVar: "color-mix(in srgb, var(--brand) 74%, var(--ink))" },
    ],
    FEED_SERIES_VARS,
  );
  // --brand-d is the brand hue darkened, so it collides with Bhusa and yields to --info; the
  // mixed shade collides too and, with --brand-d also a brand hue, finds no free colour and keeps its own.
  assert.deepEqual(out.map((s) => s.colorVar), ["var(--brand)", "var(--amber)", "var(--info)", "color-mix(in srgb, var(--brand) 74%, var(--ink))"]);
});

test("the per-item cards hide the retired splits, the unbranded and Vijay concentrates and baking soda", () => {
  const hidden = contract.match(/"chart\.item\.hidden_feeds":\s*"([^"]+)"/)?.[1];
  assert.ok(hidden, "the backend contract must author the hidden-feed rule");
  const fn = page.match(/function itemCardHidden\(rule: string, label: string\): boolean \{[\s\S]*?\n\}/)?.[0];
  assert.ok(fn, "itemCardHidden must exist in the page");
  const itemCardHidden = new Function("rule", "label", fn.replace(/function itemCardHidden\(rule: string, label: string\): boolean/, "").replace(/^\s*\{/, "").replace(/\}\s*$/, ""));
  for (const label of ["Mesha Adult Concentrate Goat", "Mesha Adult Concentrate Sheep", "Mesha Kids Goat Concentrate", "Mesha Kids Sheep Concentrate", "Concentrate", "Vijay Concentrate", "Baking Soda"]) {
    assert.equal(itemCardHidden(hidden, label), true, `${label} card must be hidden`);
  }
  for (const label of ["Mesha Adult Concentrate", "Mesha Kids Concentrate", "Dry Masoor Bhusa", "UHT Milk"]) {
    assert.equal(itemCardHidden(hidden, label), false, `${label} card must stay`);
  }
  assert.equal(itemCardHidden("", "Dry Masoor Bhusa"), false, "an empty rule hides nothing");
  assert.match(page, /\.filter\(\(\{ series \}\) => !itemCardHidden\(fa\(pageContract, "chart\.item\.hidden_feeds"\), series\.label\)\)/, "the card loop applies the rule");
});
