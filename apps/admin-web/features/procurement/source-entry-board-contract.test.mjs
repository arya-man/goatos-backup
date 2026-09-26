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

assert.match(source, /<Table className="source-loads-table">/, "source-entry loads table must keep its scoped class");
assert.match(css, /\.main table\.source-loads-table\{min-width:1180px\}/, "source-entry table must own enough width for mobile scroll");
assert.match(css, /\.main table\.source-loads-table th,\s*\.main table\.source-loads-table td,\s*\.main table\.source-loads-table td \.celllink\{white-space:nowrap/, "source-entry table links must not clip/wrap status labels");
assert.match(css, /\.main table\.source-loads-table th:nth-child\(9\),\.main table\.source-loads-table td:nth-child\(9\)\{min-width:172px\}/, "source-entry status column must fit Accepted intake");
// Same two class names, handed to the kit PagedRows that now renders this table: `.twrap` owns
// the horizontal scroll and `.procurement-load-goats-table` carries the per-column min-widths
// asserted below. PagedRows supplies the tabIndex/role/aria-label itself.
assert.match(
  loadDetail,
  /<PagedRows[\s\S]*?wrapClassName="twrap"[\s\S]*?tableClassName="procurement-load-goats-table"/,
  "load detail animal table must use the standard mobile scroll owner",
);
assert.match(css, /\.main table\.procurement-load-goats-table\{min-width:1280px\}/, "load detail animal table must stay horizontally scrollable on mobile");
