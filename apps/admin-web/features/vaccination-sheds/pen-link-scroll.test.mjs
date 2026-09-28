import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: pen-link-scrolls-to-top (TR-2 P1-11). The pen row link opens ANOTHER page (the pen detail);
// `scroll={false}` kept the board's scroll offset, so a phone landed on the detail at y=792 with its
// header off screen. Only same-page URL-state links (reset, paging) keep the scroll position.
test("the pen board's row link to the pen detail page does not keep the scroll offset", () => {
  const src = readFileSync(new URL("./shed-board.tsx", import.meta.url), "utf8");
  const rowLink = src.match(/<Link\s+href=\{href\}\s+className="shed-summary-row-link"[\s\S]*?\/>/)?.[0];
  assert.ok(rowLink, "row link found");
  assert.doesNotMatch(rowLink, /scroll=\{false\}/);
});
