import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./feed-analytics.tsx", import.meta.url), "utf8");
const directedViewBlock = source.slice(
  source.indexOf("function buildDirectedView"),
  source.indexOf("// ---------------------------------------------------------------------------", source.indexOf("function buildDirectedView") + 1),
);

test("directed feed series use indexed day-item rows instead of repeated scans", () => {
  assert.match(directedViewBlock, /const rowByDayItem = new Map<string, FeedAnalyticsDirectedResponse\["items"\]\[number\]>\(\);/);
  assert.match(directedViewBlock, /rowByDayItem\.set\(`\$\{item\.feed_day\}\\u0000\$\{item\.feed_item_key\}`, item\);/);
  assert.match(directedViewBlock, /const rowFor = \(day: string, itemKey: string\) => rowByDayItem\.get\(`\$\{day\}\\u0000\$\{itemKey\}`\);/);
  assert.match(directedViewBlock, /const row = rowFor\(day, key\);/);
  assert.doesNotMatch(directedViewBlock, /data\.items\.find/);
});

test("all feed item chart series use the shared non-repeating colour helper", () => {
  assert.match(source, /seriesColorVar,/);
  assert.match(directedViewBlock, /colorVar: seriesColorVar\(s\),/);
  assert.match(source, /entries=\{view\.itemLabels\.map\(\(label, s\) => \(\{\s*label,\s*colorVar: seriesColorVar\(s\),/s);
  assert.doesNotMatch(directedViewBlock, /colorVar: FEED_SERIES_VARS\[s % FEED_SERIES_VARS\.length\]/);
});

test("execution variance table defaults to today's packing day read", () => {
  assert.match(source, /const variancePackingDay = favDay \|\| todayIso\(\);/);
  assert.match(source, /tab === "execution"\s*\?\s*await getFeedAnalyticsExecution\(\{/s);
  assert.match(source, /date_from: istDayPlus\(variancePackingDay, 1\),/);
  assert.match(source, /date_to: istDayPlus\(variancePackingDay, 1\),/);
  assert.match(source, /day: variancePackingDay,/);
  assert.doesNotMatch(source, /tab === "execution" && favDay !== ""/);
});

test("execution variance filters reset the variance table offset", () => {
  const varianceBlock = source.slice(
    source.indexOf("{/* Intended-vs-entered packing mismatches"),
    source.indexOf("{/* The trend belongs UNDER this table", source.indexOf("{/* Intended-vs-entered packing mismatches")),
  );
  assert.match(varianceBlock, /pageParam="fav_offset"/);
  assert.match(varianceBlock, /feedHref\(PAGE_PATH, variance\.searchParams, "fav_offset"/);
  assert.doesNotMatch(varianceBlock, /pageParam="fa_offset"/);
});

// ---------------------------------------------------------------------------
// Packed vs directed — the difference tag's two tones (maintainer, 2026-08-24).
test("the difference tag is red beyond the packing tolerance and green within it", () => {
  // Two tones only. `t-info` was the old no-judgement blue; a bag now either matches the sheet
  // closely enough or it does not.
  assert.match(source, /row\.exceeds_tolerance \? "tag t-dng" : "tag t-ok"/);
  assert.doesNotMatch(source, /variance_kg\) === 0 \? "tag t-ok" : "tag t-info"/);
});

test("the renderer never applies the tolerance itself", () => {
  // The threshold is a business rule. It arrives DECIDED as `exceeds_tolerance`, from the same
  // constant the packed-vs-given trend uses; a renderer comparing 0.2 here would be a second
  // source for one rule, and the table and the trend would drift the day it changes.
  assert.doesNotMatch(source, /0\.2\b/);
  assert.doesNotMatch(source, /ToleranceKg/);
  // Over and under are both breaches, so the tone must not be conditioned on the sign.
  assert.doesNotMatch(source, /exceeds_tolerance && num\(row\.variance_kg\) [<>]/);
});

test("the tag says in words what its colour means", () => {
  // A colour alone is invisible to a screen reader and to a colour-blind reader.
  assert.match(source, /variance\.beyond_tolerance/);
  assert.match(source, /variance\.within_tolerance/);
  const contract = readFileSync(
    new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
    "utf8",
  );
  assert.match(contract, /"variance\.within_tolerance":/);
  assert.match(contract, /"variance\.beyond_tolerance":/);
});

// ---------------------------------------------------------------------------
// Today's frozen sheet (maintainer decision 2026-09-02).
test("the daily charts run through today while the rest of the page stays on yesterday", () => {
  // Today's sheet is already issued and frozen, so the kg it directs is settled fact and
  // the charts show it. The execution arm keeps the yesterday-ending window on purpose:
  // today's packing and distribution are still being worked, and counting them would read
  // as a verification failure rather than as work in progress.
  assert.match(source, /const chartWindow = rangeDates\(range, todayIso\(\)\);/);
  assert.match(source, /const chartParams = \{ park_id: parkId, \.\.\.chartWindow \};/);
  assert.match(source, /wantDirected\s*\?\s*getFeedAnalyticsDirected\(chartParams\)/s);
  assert.match(source, /wantStock\s*\?\s*getFeedAnalyticsStock\(chartParams\)/s);
  assert.match(source, /wantExecution\s*\?\s*getFeedAnalyticsExecution\(\{\s*\.\.\.params,/s);
});

test("the KPI tiles name the settled day rather than the last day drawn", () => {
  // The tiles say "Directed yesterday" / "Animals fed yesterday". Taking the array's last
  // element now points them at today — a day the farm is still feeding, whose second park's
  // sheet may not even be issued yet.
  assert.match(directedViewBlock, /latestDay: data\.days\.find\(\(d\) => d\.feed_day === settledDay\)/);
  assert.doesNotMatch(directedViewBlock, /data\.days\[data\.days\.length - 1\]/);
  assert.match(source, /buildDirectedView\(data, fa\(pageContract, "series\.other"\), istDayPlus\(todayIso\(\), -1\)\)/);
});

test("milk-only item days stay on the chart axis", () => {
  // Day totals are sheet-only by design, but item rows now include UHT Milk. A
  // milk-only day must still get an x-axis slot instead of being dropped by
  // buildDirectedView before the chart renderer ever sees it.
  assert.match(directedViewBlock, /\.\.\.data\.days\.map\(\(d\) => d\.feed_day\)/);
  assert.match(directedViewBlock, /\.\.\.data\.items\.map\(\(item\) => item\.feed_day\)/);
  assert.match(directedViewBlock, /new Set\(/);
  assert.match(directedViewBlock, /\.sort\(\)/);
  assert.doesNotMatch(directedViewBlock, /const dayKeys = data\.days\.map\(\(d\) => d\.feed_day\);/);
});

test("the stacked chart's own copy says the milk is in it", () => {
  // Backend-owned copy: the bars now carry UHT Milk alongside the sheet's feeds, so the
  // hint may no longer describe them as the issued sheet alone.
  const contract = readFileSync(
    new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
    "utf8",
  );
  assert.match(contract, /"chart\.daily\.hint":\s*"Total kg fed per day, stacked by feed item — the issued sheet plus the milk the crew prepared"/);
  assert.doesNotMatch(contract, /"chart\.daily\.hint":\s*"Total kg on the issued sheet per day/);
});
