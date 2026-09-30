// Source-shape contract tests for the Feed Analytics overview's "Feed by pen" charts
// (maintainer request 2026-09-14: one chart per pen of the chosen name, seven bars, each the
// feed directed per animal that day; model: pen-week-gain.contract.test.mjs). They pin what
// keeps the charts honest: the bar is the backend's `per_head_grams`, never a division done
// here; the pens of one name share ONE scale; a day absent from the response is a gap with
// backend copy, never a zero bar; and every visible string is a page-contract key.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "feed-shed-feed-charts.tsx"), "utf8");
const penColumns = readFileSync(new URL("./feed-pen-columns.tsx", import.meta.url), "utf8");
const columns = readFileSync(join(here, "feed-pen-columns.tsx"), "utf8");
const page = readFileSync(join(here, "feed-analytics.tsx"), "utf8");

test("the bar is the backend's per-animal figure, never a local division", () => {
  assert.match(source, /num\(day\.per_head_grams\)/, "the bar value is per_head_grams as served");
  assert.doesNotMatch(source, /directed_kg\)?\s*\/\s*|\/\s*(day|pen)\.head_count/, "no kg-over-heads division here");
  assert.match(source, /const scale = Math\.max\(1, max\)/, "one scale for every pen of the chosen name");
  assert.match(source, /for \(const pen of pens\)[\s\S]*?if \(value !== null && value > max\) max = value;/, "the scale spans every drawn pen");
});

test("an absent day is a gap with backend copy, never a zero bar", () => {
  assert.match(source, /const value = day \? num\(day\.per_head_grams\) : null;/, "a missing day is null, which the kit chart draws as no bar");
  assert.match(source, /directed: value,/, "the null reaches the chart as the series value, not a 0");
  assert.match(source, /fc\("shedfeed\.day\.gap"\)/, "the gap tooltip is backend copy");
  assert.doesNotMatch(source, /(per_head_grams|directed_kg|value|total)\)?\s*\?\?\s*0\b/, "a missing figure must never be coerced to 0");
  assert.doesNotMatch(columns, /(directed|verified)\s*\?\?\s*0\b/, "the chart wrapper must not coerce a null figure to 0 either");
});

// The columns are the kit column chart (redesign, 2026-09-20): figures on hover only, draw-in on
// first sight, the pens of one name on ONE value axis, and a missing verified figure an explicit
// mark in the hover card rather than a silent gap.
test("the pens draw as the kit column chart on one shared axis, figures on hover only", () => {
  assert.match(source, /<FeedPenColumns/, "the section mounts the kit column charts");
  assert.match(source, /yMax=\{scale\}/, "the shared scale is handed to every pen's chart");
  assert.match(columns, /<TrendChart/, "each pen is a TrendChart");
  assert.match(columns, /kind="bar"/, "drawn as columns");
  assert.match(columns, /yDomain=\{\[0, top\]\}/, "every pen's value axis is the shared domain");
  assert.match(columns, /missingLabel=\{missingLabel\}/, "a missing figure is named in the hover card");
  assert.match(source, /missingLabel="—"/, "the missing mark is the explicit dash");
  assert.doesNotMatch(columns, /LabelList|<Bar[^>]*label=/, "no static value labels on the bars: figures live in the hover card");
  assert.doesNotMatch(source, /className="penbars-value"|className="penbars-bar"/, "the legacy CSS bars are gone");
  assert.match(source, /<InfoHint text=\{fc\("shedfeed\.hint"\)\}/, "the section's meaning is the title's hint, not a paragraph");
  assert.doesNotMatch(source, /<span className="small muted">\{fc\("shedfeed\.hint"\)\}<\/span>/, "no prose under the title");
});

test("every visible string is a page-contract key and the page mounts the charts, not the table", () => {
  for (const key of [
    "shedfeed.title",
    "shedfeed.hint",
    "shedfeed.empty",
    "shedfeed.empty_filtered",
    "shedfeed.filter.shed",
    "shedfeed.chart.aria",
    "shedfeed.day.gap",
    "shedfeed.day.animals",
    "shedfeed.day.total",
    "unit.g_per_head",
  ]) {
    assert.ok(source.includes(`"${key}"`), `charts must read backend copy key ${key}`);
  }
  assert.match(page, /<FeedShedFeedCharts/, "the overview mounts the charts");
  assert.doesNotMatch(page, /FeedShedFeedTable|feed-shed-feed-table/, "the feed-mix table is retired");
  assert.match(source, /allowAll: false,\s*options: shedOptions/, "a pen name is always chosen; there is no All");
});

// The VERIFIED bar beside the DIRECTED bar (maintainer request 2026-09-18).
test("the verified bar is the backend's figure on the same scale, and a blank figure is a gap, never 0", () => {
  assert.match(source, /num\(day\.verified_per_head_grams\)/, "the verified bar is verified_per_head_grams as served");
  assert.doesNotMatch(source, /verified_kg\)?\s*\/\s*/, "no kg-over-heads division on the verified side either");
  assert.match(
    source,
    /if \(verified !== null && verified > max\) max = verified;/,
    "the shared scale spans the verified bars too, or a taller verified bar could mean less feed",
  );
  assert.match(columns, /key: "verified", label: verifiedLabel/, "the verified bar is its own series");
  assert.match(source, /fc\("shedfeed\.day\.verified_gap"\)/, "a day with no approved bag says so in backend copy");
  // `Number("")` is 0: the parser must treat a blank wire figure as absent, or an unverified
  // day draws a "0 g · 0 / 2 bags verified" bar that reads as "she measured nothing".
  assert.match(source, /if \(raw\.trim\(\) === ""\) return null;/, "a blank figure parses to null, not 0");
  // Coverage is rendered from the backend's counts, never derived here.
  assert.match(source, /\$\{day\.verified_bags\} \/ \$\{day\.planned_bags\} \$\{fc\("shedfeed\.day\.bags"\)\}/, "bags verified is backend counts + backend copy");
  for (const key of ["shedfeed.legend.directed", "shedfeed.legend.verified", "shedfeed.day.verified_gap", "shedfeed.day.bags"]) {
    assert.ok(source.includes(`"${key}"`), `charts must read backend copy key ${key}`);
  }
});

test("mobile pen bar scroll regions are keyboard focusable and labelled", () => {
  // The markup moved into the shared FeedPenColumns component; the RULE did not. `.penbars` can
  // scroll at phone width and a scroll container nothing can focus cannot be scrolled from a
  // keyboard, so the group + tabIndex are asserted where the element now lives. This page still
  // composes the label (pen + chart aria) and hands it over.
  // Template anatomy: each pen is an sx Box (divider border, neutral surface), never a legacy .penbars class.
  assert.match(penColumns, /<Box\s+key=\{pen\.key\}\s+role="group"\s+tabIndex=\{0\}/);
  assert.doesNotMatch(penColumns, /className="(qgrid|penbars)/);
  assert.match(penColumns, /aria-label=\{pen\.ariaLabel\}/);
  assert.match(source, /ariaLabel: `\$\{pen\.operational_location_display\} · \$\{fc\("shedfeed\.chart\.aria"\)\}`/);
});
