import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: url-panel-no-timer (J3 P0-1). The panel skeleton commits with the pending state itself; a
// timer between the click and the skeleton starved behind the router transition, leaving the old
// panel under the new tab with aria-busy and no skeleton.
const src = readFileSync(new URL("./url-panel.tsx", import.meta.url), "utf8");

test("guard: url-panel-no-timer - the fallback shows as soon as the panel is pending", () => {
  assert.match(src, /const showFallback = pending;/);
  assert.doesNotMatch(src, /shownFor|URL_PANEL_SKELETON_DELAY_MS/);
  assert.match(src, /data-url-panel-pending=\{showFallback \? "" : undefined\}/);
});
