import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./feed-analytics.tsx", import.meta.url), "utf8");
const directedViewBlock = source.slice(
  source.indexOf("function buildDirectedView"),
  source.indexOf("// ---------------------------------------------------------------------------", source.indexOf("function buildDirectedView") + 1),
);
const stockCheckBlock = source.slice(
  source.indexOf("function StockCheckTag"),
  source.indexOf("\n// ---------------------------------------------------------------------------", source.indexOf("function StockCheckTag")),
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

test("stock check displays days left from stock divided by latest-3-day average", () => {
  assert.match(stockCheckBlock, /row\.stock_variance_kg/);
  assert.match(stockCheckBlock, /const daysLeft = Math\.floor\(num\(row\.stock_variance_kg\)\);/);
  assert.match(stockCheckBlock, /row\.stock_check_status === "mismatch" \? "t-dng" : "t-ok"/);
  assert.match(stockCheckBlock, /stock\.days_left/);
  assert.doesNotMatch(stockCheckBlock, /unit\.kg/);
});

test("farm stock table does not render total purchased as a visible column", () => {
  assert.doesNotMatch(source, /stock\.farms\.col\.total/);
  assert.doesNotMatch(source, /row\.total_purchased_kg/);
});

test("farm stock table displays ledger stock, not projected shortage", () => {
  assert.match(source, /row\.ledger_stock_kg === "" \? "—" : `\$\{nf\(num\(row\.ledger_stock_kg\)\)\} \$\{fa\(pageContract, "unit\.kg"\)\}`/);
  assert.doesNotMatch(source, /ProjectedStockText/);
  assert.doesNotMatch(source, /Short \$\{nf/);
});
