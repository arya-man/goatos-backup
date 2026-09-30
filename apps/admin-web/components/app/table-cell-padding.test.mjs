// Guard: template-table-padding (TR1-#19). Table cells take the template's own padding (MUI
// TableCell 16px, `size="small"` / Dense 6px 16px); no global stylesheet re-pads every table's first
// / last cell to 24px or pins header heights. Measured: /counts/herd first cell 24px -> 16px, rows
// 77px with the avatar lead cell like the template user list.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

const css = legacyCss("frame", "minimal-theme", "mesha-theme");

test("template-table-padding: no global first/last-cell re-padding", () => {
  assert.doesNotMatch(css, /\.wrap (?:thead th|tbody td):first-child\{padding-left:24px\}/);
  assert.doesNotMatch(css, /\.wrap (?:thead th|tbody td):last-child\{padding-right:24px\}/);
  assert.doesNotMatch(css, /\.wrap thead th\{height:57px/);
});
