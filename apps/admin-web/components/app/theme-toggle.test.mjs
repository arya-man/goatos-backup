import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// PR #294 L3/K9: the header theme glyph showed the sun on some light pages and the moon on others,
// depending on when hydration finished an AnimatePresence exit. The glyph must be decided by CSS
// from html[data-theme] (set by the boot script before first paint), never by a React key swap.
const src = readFileSync(new URL("./theme-toggle.tsx", import.meta.url), "utf8");

test("theme glyph is a pure function of data-theme", () => {
  assert.doesNotMatch(src, /AnimatePresence/, "no exit/enter swap of the glyph");
  assert.doesNotMatch(src, /key=\{mode\}/, "the glyph is not keyed by the hydrated mode");
  assert.match(src, /:root\[data-theme="light"\] & \[data-glyph="to-dark"\]/);
  assert.match(src, /:root\[data-theme="light"\] & \[data-glyph="to-light"\]/);
  assert.doesNotMatch(src, /\{isLight \? </, "the rendered glyph never branches on the hydrated mode");
});
