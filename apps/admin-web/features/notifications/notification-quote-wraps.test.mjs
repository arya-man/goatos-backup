import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

// Comments stripped first: this file's own explanation names the retired selector, and a naive
// scan would match the prose instead of the rule.
const frame = legacyCss("frame").replace(/\/\*[\s\S]*?\*\//g, "");
const panel = readFileSync(new URL("./notification-panel.tsx", import.meta.url), "utf8");

// The notification body is a two-line clamp with the full text on `title` until the row is
// opened. It rendered as ONE line, cut mid-word, with no ellipsis -- measured in Chrome at 848px
// of text inside a 276px box -- because `.kit-sheet button { white-space: nowrap }` matched the
// row's own expand toggle and `white-space` INHERITS into the quote. A -webkit-line-clamp can
// never reach a second line when the text may not wrap, so the clamp silently did nothing.
//
// The kit Sheet (and its scoped nowrap rule) is gone: the centre is the template MUI Drawer. What
// must never come back is a drawer-wide `button` nowrap that a content row inherits.
test("no drawer-wide button nowrap rule reaches the notification rows", () => {
  assert.doesNotMatch(frame, /\.kit-sheet\b/, "the retired kit Sheet selectors stay deleted");
  // The expand toggle is a ButtonBase that sets `whiteSpace: "normal"` itself, so no inherited
  // nowrap can reach the quote.
  assert.match(panel, /<ButtonBase[\s\S]*?whiteSpace: "normal"/);
});

test("the notification quote still declares the two-line clamp it depends on", () => {
  assert.match(panel, /data-notification-quote[\s\S]*?WebkitLineClamp: 2/);
});
