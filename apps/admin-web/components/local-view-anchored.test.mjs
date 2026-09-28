import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: local-view-anchored (TR2-P2-12). Clicking the /weighing/weights "Table" view tab moved the
// strip 725px: the chart pane had the switch at its foot, the table pane in its CardHeader. The
// toggle re-anchors the page on the copy that becomes visible, and both weights panes keep it at
// the card foot.
const toggle = readFileSync(new URL("./local-view-switch.tsx", import.meta.url), "utf8");
const weights = readFileSync(new URL("../features/weighing/weights.tsx", import.meta.url), "utf8");

test("guard: local-view-anchored - the toggle scrolls the visible copy back under the pointer", () => {
  assert.match(toggle, /data-local-view-toggle=\{param\}/);
  assert.match(toggle, /beforeTop = before\?\.getBoundingClientRect\(\)\.top/);
  assert.match(toggle, /window\.scrollBy\(\{ top: after\.getBoundingClientRect\(\)\.top - beforeTop/);
});

test("guard: local-view-anchored - the weights gain switch never sits in a CardHeader action", () => {
  assert.doesNotMatch(weights, /action=\{gainViewSwitch\}/);
  assert.equal((weights.match(/<Box sx=\{\{ (?:px: 3, pb: 3|p: 3) \}\}>\{gainViewSwitch\}<\/Box>/g) ?? []).length, 2);
});
