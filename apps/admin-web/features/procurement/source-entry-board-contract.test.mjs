import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "source-entry-board.tsx"), "utf8");

assert.match(
  source,
  /Promise\.all\(loads\.map\(async \(load\) => \[load\.load_id, await getProcurementLoad\(load\.load_id\)\] as const\)\)/,
  "source-entry list must fetch details for every visible row while purpose/tagging/HF/goat-count columns depend on detail-only data",
);

assert.doesNotMatch(
  source,
  /if \(selectedLoadId && loads\.some\([\s\S]*?getProcurementLoad\(selectedLoadId\)/,
  "source-entry list must not fetch only the selected load detail while rendering detail-derived columns for every row",
);
