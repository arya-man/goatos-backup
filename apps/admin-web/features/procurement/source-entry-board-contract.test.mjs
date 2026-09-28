import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "source-entry-board.tsx"), "utf8");
const loadDetail = readFileSync(join(here, "load-detail.tsx"), "utf8");
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
// 1480px in a 1060px card from legacy mesha-theme.css min-widths): no legacy width rule, headings
// and text wrap at word breaks with status Labels whole, the table sits in the template Scrollbar
// (960px floor, scrolls inside the card below that), the nine work-state tabs are the template
// scrollable Tabs WITH scroll arrows, and the footer is the template pager (rows-per-page, Dense).
assert.doesNotMatch(css, /table\.source-loads-table/, "no legacy mesha-theme.css width/nowrap rule on the source-entry table");
assert.match(source, /<Scrollbar>\s*<Table className="source-loads-table" sx=\{SOURCE_LOADS_TABLE_SX\}>/, "loads table inside the template Scrollbar");
assert.match(source, /const SOURCE_LOADS_TABLE_SX = \{\s*minWidth: 960,\s*"& thead th, & tbody td, & tbody td \.celllink": \{ whiteSpace: "normal", overflowWrap: "normal", wordBreak: "normal" \}/, "table text wraps at word breaks above the 960px floor");
assert.match(source, /"& \.minimal__label__root, & \.MuiChip-root": \{ whiteSpace: "nowrap" \}/, "status Labels never split");
assert.match(source, /<UrlTabs\s+scrollButtons="auto"/, "work-state tabs are scrollable with arrows (no clipped last tab)");
assert.match(source, /<ProcurementPager[\s\S]*?dense[\s\S]*?rowsPerPage=\{PAGE_SIZE\}[\s\S]*?rowsPerPageHrefs=/, "template pager: Dense + rows-per-page");
assert.match(source, /rowsPerPageHrefs=\{pageSizes\.map\(\(size\) => \(\{ value: size, href: hrefWithQuery\(pathname, sp, \{ limit: String\(size\), cursor: null, cursor_stack: null, page: null/, "a page-size change restarts the cursor chain");
// Same two class names, handed to the kit PagedRows that now renders this table: `.twrap` owns
// the horizontal scroll and `.procurement-load-goats-table` carries the per-column min-widths
// asserted below. PagedRows supplies the tabIndex/role/aria-label itself.
assert.match(
  loadDetail,
  /<PagedRows[\s\S]*?wrapClassName="twrap"[\s\S]*?tableClassName="procurement-load-goats-table"/,
  "load detail animal table must use the standard mobile scroll owner",
);
assert.match(css, /\.main table\.procurement-load-goats-table\{min-width:1280px\}/, "load detail animal table must stay horizontally scrollable on mobile");
