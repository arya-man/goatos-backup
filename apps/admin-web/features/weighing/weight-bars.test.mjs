import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const component = readFileSync(new URL("./weight-bars.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

test("bars render through the kit bar list with the mode chip as a note beside the label", () => {
  // The list is the kit's BarList (the MUI Minimal template item: LinearProgress track, zero-baseline
  // axis track for losses, template Tooltip on each row); the mode chip rides as `note`, never inside
  // the label text, so the clamped label can never swallow it.
  assert.match(component, /import \{ BarList \} from "@\/components\/bar-list";/);
  assert.match(component, /note: bar\.modeLabel \? <Tag tone=\{bar\.modeTone \?\? "mut"\}>\{bar\.modeLabel\}<\/Tag> : undefined,/);
  assert.match(component, /labelText: stageLabel\(bar\.label\),/);
  assert.doesNotMatch(component, /className="wbl-text"/);
  // The list never drops a losing row: only non-finite values are filtered.
  assert.match(component, /const bars = data\.filter\(\(bar\) => Number\.isFinite\(bar\.value\)\);/);
  assert.match(css, /\.wbars-empty\{/);
});
