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
  assert.match(source, /const \[locations, directed, execution, experiment, stock, shedFeed, loads\] = await Promise\.all\(\[/);
  assert.match(source, /const executionDay =\s*tab === "execution"\s*\?\s*await getFeedAnalyticsExecution\(\{/s);
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
  assert.match(source, /wantDirected\s*\?\s*getFeedAnalyticsDirected\(\{ \.\.\.chartParams, sections: directedSections \}\)/s);
  assert.match(source, /tab === "overview"\s*\?\s*"expenditure,spend,item_expenditure"/s);
  // The Stock tab's arms no longer branch on stockOnly: both readers of that tab
  // render the same three, and item_expenditure moved to the tab that reads it.
  assert.match(source, /:\s*"items,farm_items,forecast";/s);
  assert.match(source, /wantStock\s*\?\s*getFeedAnalyticsStock\(\{ \.\.\.chartParams, sections: stockSections \}\)/s);
  assert.match(source, /wantExecution\s*\?\s*getFeedAnalyticsExecution\(\{\s*\.\.\.params,/s);
});

test("stock-only feed analytics hides full controls and item graphs", () => {
  // Procurement Director receives a one-option page contract. That surface should read stock only:
  // no explanatory banner, no tab/range controls, no directed-feed graph grid below the stock table.
  // Stock-scope only means "no Consumption tab offered": the Procurement Director's contract now
  // carries Stock AND Purchased vs consumed (both ride the stock permission), so the rule keys on
  // the tab that needs the full feed read rather than on a count of one.
  assert.match(source, /const stockOnly = !allowedTabs\.includes\("overview"\);/);
  assert.match(source, /const wantDirected = !stockOnly && \(tab === "overview" \|\| tab === "items" \|\| tab === "peranimal"\);/);
  assert.match(source, /\{!stockOnly \? \(\s*<>\s*<p className="muted small"[\s\S]*?<SegmentedLinks[\s\S]*?<SegmentedLinks[\s\S]*?<\/>\s*\) : null\}/);
  assert.match(source, /const nonNull = \[directed, execution, experiment, stock, shedFeed, executionDay, loads\]\.filter/s);
  assert.match(source, /const gated = \[directed, execution, experiment, shedFeed, loads, tab === "execution" \? executionDay : null, stockOnly \? stock : null\];/s);
  assert.match(source, /\{stockOnly && tab === "items" && !failed \? \(\s*<StockCards stock=\{stock\?\.ok \? stock\.data : null\} pageContract=\{pageContract\} \/>/s);
  // The per-feed money cards moved to the Consumption tab (maintainer request 2026-09-04). A
  // stock-only reader's contract offers no such tab, so the Stock tab carries the stock table alone.
  assert.match(source, /\{tab === "overview" \? \(\s*\/\/ Consumption tab/);
  assert.doesNotMatch(source, /\{tab === "items" && !stockOnly \? \(/);
  // The spend-share pie sits on the Consumption tab too, fed by the same per-item money as the
  // cards (average ₹ per priced day), so the slice and the card strip can never disagree.
  // The ranked, priced items feed the pie through distinctSliceColors, which only re-colours a
  // slice whose hue is already taken; the slice VALUE is still the item's ₹ per priced day.
  assert.match(source, /const spendShareSlices: PieSlice\[\] = distinctSliceColors\(\s*rankItemCards\(view\.itemSeries, itemMoney\)[\s\S]*?money\.rupeesTotal \/ money\.pricedDays/);
  // Gated on the FILTERED slices: when the feed rule leaves nothing priced, the pie is hidden rather
  // than captioned with the directed-feed empty copy (review on PR #187).
  assert.match(source, /\{tab === "overview" && spendShareSlices\.length > 0 \? \([\s\S]*?<FeedSpendPie[\s\S]*?slices=\{spendShareSlices\}/);
});

test("the KPI tiles name the settled day rather than the last day drawn", () => {
  // The tiles say "Directed yesterday" / "Animals fed yesterday". Taking the array's last
  // element now points them at today — a day the farm is still feeding, whose second park's
  // sheet may not even be issued yet.
  assert.match(directedViewBlock, /latestDay: data\.days\.find\(\(d\) => d\.feed_day === settledDay\)/);
  assert.doesNotMatch(directedViewBlock, /data\.days\[data\.days\.length - 1\]/);
  assert.match(source, /buildDirectedView\(data, fa\(pageContract, "series\.other"\), istDayPlus\(todayIso\(\), -1\)\)/);
});

test("the cost-per-animal tile divides yesterday's spend by yesterday's animals, same settled day", () => {
  // ₹ per animal per day = the priced expenditure of the SETTLED day over the sheet's head
  // count for that same day. Matching on feed_day is what keeps the two halves on one day;
  // a positional last element would price today's half-issued sheet against yesterday's herd.
  assert.match(source, /stock\.expenditure\.find\(\(d\) => d\.feed_day === latest\.feed_day\)/);
  assert.match(source, /latest\.head_days > 0/);
  assert.match(source, /rate\(num\(spentDay\.rupees\) \/ latest\.head_days\)/);
  // No priced day or no animals reads "—", never ₹0.
  assert.match(source, /\{costPerAnimal \?\? "—"\}/);
  assert.match(source, /"kpi\.cost_per_animal\.label"/);
});

test("the expenditure chart's per-animal reading divides each day by that day's own animals", () => {
  // The toggle is a URL param read like the range (absent = overall), and its labels come
  // from the page contract, never a local literal.
  assert.match(source, /const SPEND_MODES = \["overall", "per_animal"\] as const/);
  assert.match(source, /const spendMode = readSpendMode\(searchParams\)/);
  assert.match(source, /href: hrefWith\(searchParams, \{ spend: m === "overall" \? undefined : m \}\)/);
  assert.match(source, /`chart\.spend\.mode\.\$\{m\}`/);
  // Per animal = that day's ₹ over that day's head count, matched on feed_day — a positional
  // zip would pair the wrong days whenever the two series start on different dates. No
  // animals on the sheet means no figure (a broken line), never ₹0.
  assert.match(source, /data\.days\.find\(\(x\) => x\.feed_day === d\.feed_day\)/);
  assert.match(source, /day && day\.head_days > 0 \? num\(d\.rupees\) \/ day\.head_days : null/);
  // Overall stays exactly the series it was.
  assert.match(source, /points: stock\.expenditure\.map\(\(d\) => num\(d\.rupees\)\)/);
  assert.match(source, /spendMode === "per_animal" \? "unit\.rupees_per_animal" : "unit\.rupees"/);
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

// A tab that does not ASK for a stock arm is served an empty one, silently and
// with no error, so a narrowing that omits an arm the tab renders is invisible
// to typecheck and to every rendering test. It shipped once: Consumption asked
// for "expenditure,spend" while item_expenditure -- the only source of per-item
// money -- was requested by the Stock tab, which never reads it. The spend-share
// pie hid itself (it is gated on having slices), every per-item card fell back
// to its unpriced state, and the spend ranking collapsed to input order.
test("the Consumption tab requests every stock arm it renders, item money included", () => {
  const overviewSections = source.match(/tab === "overview"\s*\n\s*\? "([a-z_,]+)"/);
  assert.ok(overviewSections, "the stock sections narrowing must name the overview tab's arms");
  const asked = new Set(overviewSections[1].split(","));
  // buildItemMoney reads item_expenditure and is built for the overview tab alone.
  assert.match(source, /const itemMoney = tab === "overview" \? buildItemMoney\(stock,/);
  assert.match(source, /for \(const row of stock\.item_expenditure \?\? \[\]\)/);
  for (const arm of ["expenditure", "spend", "item_expenditure"]) {
    assert.ok(asked.has(arm), `the Consumption tab renders ${arm} and must request it`);
  }
});

// The Stock tab reads items/farm_items/forecast and nothing else; paying for
// item_expenditure there is the same mistake pointing the other way.
test("the Stock tab requests its own arms and does not pay for item money", () => {
  const stockTabSections = source.match(/: "(items[a-z_,]*)";/);
  assert.ok(stockTabSections, "the stock sections narrowing must name the Stock tab's arms");
  const asked = new Set(stockTabSections[1].split(","));
  for (const arm of ["items", "farm_items", "forecast"]) {
    assert.ok(asked.has(arm), `the Stock tab renders ${arm} and must request it`);
  }
  assert.ok(!asked.has("item_expenditure"), "the Stock tab never builds itemMoney");
});

test("Consumption's Status-wise view reads only the pen-tag arm and skips General's reads", () => {
  // Status-wise is the average directed feed per animal per pen tag (maintainer request
  // 2026-09-17). It must not pay for General's KPI, stock, spend or pen-chart reads, and General
  // must not pay for the pen-tag query.
  assert.match(source, /const directedSections = statusWise \? "pen_tags" : "days,items";/);
  assert.match(source, /const wantExecution = \(tab === "overview" && !statusWise\) \|\| tab === "execution";/);
  assert.match(source, /const wantStock = \(tab === "overview" && !statusWise\) \|\| tab === "items";/);
  assert.match(source, /const wantShedFeed = tab === "overview" && !statusWise;/);
  assert.match(source, /\{!stockOnly && statusWise && directed\?\.ok \? \(\s*<FeedStatusWise /);
  assert.match(source, /\{!stockOnly && !statusWise && directed\?\.ok && \(tab === "overview"/);
  // Only single-status categories are shown (maintainer 2026-09-17: no mixed-tag pens section),
  // filtered on the BACKEND's flag.
  assert.match(source, /const single = data\.pen_tags\.filter\(\(t\) => !t\.mixed\);/);
  assert.doesNotMatch(source, /status\.mixed\./);
  assert.doesNotMatch(source, /pen_tag_label\.includes\("\+"\)/);
});
