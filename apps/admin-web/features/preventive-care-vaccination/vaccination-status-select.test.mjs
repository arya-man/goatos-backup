import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: vaccination-status-select (TR2-P2-6). The /vaccination Command Board status filter was a
// wrapping cloud of dot pills (three extra rows at 390). It is the template UserTableToolbar multi
// Select (checkbox items + the state dot) in the filter row, mirrored by the skeleton.
const view = readFileSync(new URL("./command-board-view.tsx", import.meta.url), "utf8");
const skel = readFileSync(new URL("./vaccination-skeletons.tsx", import.meta.url), "utf8");

test("guard: vaccination-status-select - status filter is a multi Select, not chips", () => {
  const bar = view.slice(view.indexOf("const filterBar = ("), view.indexOf("// Cohort matrix", view.indexOf("const filterBar = (")) > 0 ? view.indexOf("// Cohort matrix", view.indexOf("const filterBar = (")) : view.indexOf("const filterBar = (") + 6000);
  assert.match(bar, /multiple: true/);
  assert.match(bar, /command_board\.filter\.status/);
  assert.doesNotMatch(bar, /<Chip\b/);
  assert.match(skel, /fields=\{\[CB_VACCINE_FIELD_MIN, CB_DRIVE_FIELD_MIN, CB_STATUS_FIELD_MIN\]\}/);
  assert.doesNotMatch(skel, /CB_STATUS_CHIP/);
});
