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
