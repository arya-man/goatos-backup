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

// guard: url-panel-holds-page-height (FIXJ4, /operations/audit "tab moved 108px" + "actor filter
// scrolled 562px"). A scrolled page keeps its height from the click until every pending panel shows
// content, so the swap never clamps the page to the top; the floor is released afterwards.
test("url-panel-holds-page-height: the click holds the page height until the panel painted content", () => {
  const src = readFileSync(new URL("./url-panel.tsx", import.meta.url), "utf8");
  assert.match(src, /function holdPageHeight\(\)/);
  assert.match(src, /document\.body\.style\.minHeight = `\$\{document\.documentElement\.scrollHeight\}px`/);
  assert.match(src, /window\.scrollY > 0/, "only a scrolled page is held");
  assert.match(src, /holdPageHeight\(\);\s*\}\s*queueMicrotask/, "the hold starts at the click, before the skeleton commits");
  assert.match(src, /querySelector\("\.MuiSkeleton-root, \[data-skel\]"\)/, "released once no skeleton is left in the panel");
  assert.match(src, /document\.body\.style\.minHeight = "";/, "the floor is released");
});

// guard: url-panel-click-after-react (FIXJ4). The panel hears link clicks and GET submits AFTER React
// dispatched them (window, bubble phase): swapping to the skeleton in a capture listener unmounted a
// link inside the panel before next/link handled it, and the browser reloaded the whole document.
test("url-panel-click-after-react: link clicks / submits are heard after React, never in capture", () => {
  const src = readFileSync(new URL("./url-panel.tsx", import.meta.url), "utf8");
  assert.match(src, /window\.addEventListener\("click", onClick\);/);
  assert.match(src, /window\.addEventListener\("submit", onSubmit\);/);
  assert.doesNotMatch(src, /addEventListener\("(click|submit)", on(Click|Submit), true\)/);
});

// guard: url-panel-click-path (FIXJ11, J3B N-P1-2). A pager arrow swaps its icon for a spinner in
// the Link's own click handler, so the tapped <svg> is detached before this window listener runs:
// `event.target.closest("a[href]")` found nothing and the /people directory never showed its
// skeleton on a pager tap. The anchor comes from the event path (kept at dispatch time).
test("url-panel-click-path: the clicked link is read from composedPath, not target.closest", () => {
  const src = readFileSync(new URL("./url-panel.tsx", import.meta.url), "utf8");
  assert.match(src, /const anchor = event\.composedPath\(\)\.find\(\(node\): node is HTMLAnchorElement => node instanceof HTMLAnchorElement && node\.hasAttribute\("href"\)\);/);
  assert.doesNotMatch(src, /event\.target as Element \| null\)\?\.closest\?\.\("a\[href\]"\)/);
});
