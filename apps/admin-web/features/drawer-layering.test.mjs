import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { legacyCss } from "../scripts/lib/legacy-css.mjs";

const css = legacyCss("mesha-theme");
const shell = readFileSync(new URL("../components/app/drawer/minimal-drawer.tsx", import.meta.url), "utf8");

test("side drawer backdrop stays below the drawer and does not blur the page", () => {
  // The legacy `button.scrim` had no users left and its rules were deleted (FIXJ-CI dead-selector
  // sweep); it must not come back, blurred or not.
  assert.doesNotMatch(css, /(?:^|[\s,}])(?:button)?\.scrim[\s{.,:]/);
  // Right drawers are the template temporary MUI Drawer (Ravi R2-4): the Modal stacks its own
  // backdrop under the paper, and the backdrop is always visible (no invisible prop).
  assert.match(shell, /<Drawer\s+anchor="right"/);
  assert.doesNotMatch(shell, /invisible\s*:|invisibleBackdrop/);
  // The legacy hand-rolled shell is gone for good.
  assert.doesNotMatch(css, /aside\.drawer\s*\{/);
});
