import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "source-entry-board.tsx"), "utf8");

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
  /goatsInLoad: detail \? String\(detail\.goats\.length\) : copy\(pageContract, "label\.placeholder"\)/,
  "drawer goat count must render unavailable when detail was not fetched, not 0",
);
