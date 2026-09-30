import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { anchorDelta } from "./pager-anchor.ts";

// guard: pager-under-thumb (FIXJ11, J3B P2-2): page 2 with taller rows moved the /counts/breakdown
// pager 78px under the thumb. Both pagers remember the tap and scroll the page back by the delta.
test("pager-under-thumb: the page scrolls by the pager's move, once, only for a fresh on-screen tap", () => {
  assert.equal(anchorDelta({ top: 348, at: 0 }, 426, 500, 844), 78);
  assert.equal(anchorDelta({ top: 684, at: 0 }, 624, 500, 844), -60);
  assert.equal(anchorDelta({ top: 348, at: 0 }, 349, 500, 844), 0, "a 1px wobble is not a move");
  assert.equal(anchorDelta({ top: 348, at: 0 }, 426, 20_000, 844), 0, "a stale tap never scrolls");
  assert.equal(anchorDelta({ top: 900, at: 0 }, 1000, 10, 844), 0, "a pager tapped off screen is not under a thumb");
  assert.equal(anchorDelta(null, 426, 10, 844), 0);
});

test("pager-under-thumb: TablePaginationLinks and TableFooter both use it", () => {
  const links = readFileSync(new URL("./table-pagination-links.tsx", import.meta.url), "utf8");
  assert.match(links, /usePagerAnchor\(rootRef\)/);
  assert.match(links, /onClickCapture=\{\(event\) => \{\s*if \(\(event\.target as Element\)\.closest\('a\[href\]'\)\) rememberPagerTap\(event\.currentTarget\);/);
  const footer = readFileSync(new URL("../table-footer.tsx", import.meta.url), "utf8");
  assert.match(footer, /const rememberTap = useClientPagerAnchor\(rootRef, current\);/);
  assert.match(footer, /rememberTap\(\);\s*onPageChange\(next \+ 1\);/);
});
