import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "source-entry-board.tsx"), "utf8");
const loadDetail = readFileSync(join(here, "load-detail.tsx"), "utf8");
const skeletons = readFileSync(join(here, "source-entry-skeletons.tsx"), "utf8");
const { SOURCE_LOAD_TAB_STATES } = await import("./source-entry-layout.ts");
const css = readFileSync(join(here, "../../app/mesha-theme.css"), "utf8");

assert.match(
  source,
  /if \(selectedLoadId && loads\.some\([\s\S]*?getProcurementLoad\(selectedLoadId\)/,
  "source-entry list may fetch detail for the selected drawer row only until the list API exposes row facets",
);

assert.doesNotMatch(
  source,
  /Promise\.all\(loads\.map\(async \(load\) => \[load\.load_id, await getProcurementLoad\(load\.load_id\)\] as const\)\)/,
  "source-entry page load must not fire one full-detail request per visible row",
);

assert.match(
  source,
  /function taggingLabel[\s\S]*?if \(!detail\) return copy\(pageContract, "label\.placeholder"\);/,
  "tagging must render unavailable when detail was not fetched, not 0/expected",
);

assert.match(
  source,
  /function hfVaccinationLabel[\s\S]*?if \(!detail\) \{[\s\S]*?label: copy\(pageContract, "label\.placeholder"\), tone: "mut"/,
  "HF vaccination must render unavailable when detail was not fetched, not due",
);

assert.match(
  source,
  /goatsInLoad: detail \? String\(\(detail\.goats \?\? \[\]\)\.length\) : copy\(pageContract, "label\.placeholder"\)/,
  "drawer goat count must render unavailable when detail was not fetched, not 0",
);

assert.match(source, /<Table className="source-loads-table"[ >]/, "source-entry loads table must keep its scoped class");
// guard: source-entry-table-template (TR2 P1-6). The loads table fits its card at 1440 (it drew
// 1480px in a 1060px card from legacy mesha-theme.css min-widths): no legacy width rule, status
// Labels whole, the table sits in the template Scrollbar (scrolls inside the card when wider), and
// the footer is the template pager (rows-per-page, Dense).
// guard: source-entry-tabs-fit (TR3-P1-2). The work-state strip scrolled with arrows (8+ states) and
// the head wrapped to 3 lines. Now: All + <=4 stage tabs, no scroll arrows, a status select listing
// EVERY stage, and the head keeps the template TableHeadCustom nowrap (no thead whiteSpace override).
assert.doesNotMatch(css, /table\.source-loads-table/, "no legacy mesha-theme.css width/nowrap rule on the source-entry table");
assert.match(source, /<Scrollbar>\s*<Table className="source-loads-table" sx=\{SOURCE_LOADS_TABLE_SX\}>/, "loads table inside the template Scrollbar");
assert.match(source, /const SOURCE_LOADS_TABLE_SX = \{\s*minWidth: 960,/, "table keeps its floor and scrolls inside the card below it");
assert.doesNotMatch(source.slice(source.indexOf("const SOURCE_LOADS_TABLE_SX")), /thead/, "head cells keep the template nowrap (one line)");
assert.match(source, /"& \.minimal__label__root, & \.MuiChip-root": \{ whiteSpace: "nowrap" \}/, "status Labels never split");
{
  const tabs = source.slice(source.indexOf("<UrlTabs"), source.indexOf("/>\n\n        <OrderTableToolbar"));
  assert.doesNotMatch(tabs, /scrollButtons/, "stage tabs fit the card: no scroll arrows");
  assert.match(tabs, /SOURCE_LOAD_TAB_STATES/, "tabs = All + the declared in-flight stages");
  assert.ok(SOURCE_LOAD_TAB_STATES.length <= 4, "at most All + 4 tabs (template order list)");
  const select = source.slice(source.indexOf("<LinkSelect"), source.indexOf("</Box>", source.indexOf("<LinkSelect")));
  assert.match(select, /\.\.\.sourceLoadStatuses\.map\(/, "the status select lists EVERY stage");
  assert.match(skeletons, /<TabsSkeleton count=\{1 \+ SOURCE_LOAD_TAB_STATES\.length\} \/>/, "the skeleton draws the same tab count");
  assert.match(skeletons, /<OrderToolbarSkeleton filters=\{1\}/, "the skeleton draws the status select");
}
assert.match(source, /<ProcurementPager[\s\S]*?dense[\s\S]*?rowsPerPage=\{PAGE_SIZE\}[\s\S]*?rowsPerPageHrefs=/, "template pager: Dense + rows-per-page");
assert.match(source, /rowsPerPageHrefs=\{pageSizes\.map\(\(size\) => \(\{ value: size, href: hrefWithQuery\(pathname, sp, \{ limit: String\(size\), cursor: null, cursor_stack: null, page: null/, "a page-size change restarts the cursor chain");
// main's mobile-scroll fix (56b3da919) as theme sx on the kit PagedRows (FIXJ6: the `.twrap` /
// `.procurement-load-goats-table` classes died with mesha-theme.css): the wrapper owns the sideways
// scroll, the table keeps its 1280px floor and per-column min-widths.
assert.match(
  loadDetail,
  /<PagedRows[\s\S]*?tableMinWidth=\{LOAD_GOATS_TABLE_MIN_WIDTH\}[\s\S]*?tableSx=\{LOAD_GOATS_TABLE_SX\}/,
  "load detail animal table must use the standard mobile scroll owner",
);
assert.match(loadDetail, /const LOAD_GOATS_TABLE_MIN_WIDTH = 1280;/, "load detail animal table must stay horizontally scrollable on mobile");

// TR-2 P1-6 follow-up: the New load supplier picker is built from the loads on the current page, so
// the board must default to the contract's LARGEST page size (the backend keeps 200 in
// source-loads page_size_options, TestSourceEntryLoadsPageSizesKeep200).
assert.match(source, /const PAGE_SIZE = pageSizes\.includes\(requestedLimit\) \? requestedLimit : pageSizes\[pageSizes\.length - 1\];/);
assert.match(source, /const suppliers = Array\.from\(\s*new Map\(loads\./);
