import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const procurementRoute = readFileSync(new URL("../../app/(admin)/procurement/vendors/page.tsx", import.meta.url), "utf8");
const salesRoute = readFileSync(new URL("../../app/(admin)/sales/vendors/page.tsx", import.meta.url), "utf8");
const board = readFileSync(new URL("./vendor-board.tsx", import.meta.url), "utf8");
const filterBar = readFileSync(new URL("./vendor-filter-bar.tsx", import.meta.url), "utf8");

// The maintainer's requirement for the 2026-09-05 register split, in his words: the Vendors screen
// under Procurement and under Sales must be THE SAME screen. The structural guarantee of that is
// that there is only ONE of them -- both routes mount the same component, which draws every label
// from a backend copy map the two pages share.
//
// A second component would look identical on the day it was forked and drift on the next edit, and
// nothing in the type system would notice. This test is what makes the fork fail loudly.
test("both vendor routes mount the one shared board", () => {
  for (const [name, source] of [["procurement", procurementRoute], ["sales", salesRoute]]) {
    assert.match(source, /<VendorBoardPage\b/, `${name} route must render VendorBoardPage`);
    assert.match(
      source,
      /from "@\/features\/procurement"/,
      `${name} route must import the board from the one procurement feature barrel`,
    );
  }
  // Only the page-contract key differs -- which is exactly the thing that carries the side, the
  // crumb and the register-specific empty states.
  assert.match(procurementRoute, /requireAdminWebPageContract\("vendors"\)/);
  assert.match(salesRoute, /requireAdminWebPageContract\("sales-vendors"\)/);
});

// The board must not hardcode the procurement route anywhere, or every link, filter, pager and
// drawer on Sales > Vendors silently navigates to Procurement > Vendors -- a screen that looks
// right until it is clicked.
test("the shared board and its filter bar route by contract, never by a hardcoded path", () => {
  assert.match(board, /const pathname = pageContract\.href/, "the board must take its route from its own contract");
  assert.doesNotMatch(
    board.replace(/\/\*[\s\S]*?\*\/|(^|[^:])\/\/.*$/gm, "$1"),
    /"\/procurement\/vendors"/,
    "the board must not hardcode the procurement route",
  );
  assert.doesNotMatch(
    filterBar.replace(/\/\*[\s\S]*?\*\/|(^|[^:])\/\/.*$/gm, "$1"),
    /"\/procurement\/vendors"/,
    "applying a filter must keep the person on the register they are looking at",
  );
  assert.match(filterBar, /pathname:\s*string/, "the filter bar must take the route as a prop");
});

// Both reads are scoped by the side the contract declares. Missing either one is a different bug:
// an unscoped LIST shows the wrong vendors, an unscoped CATALOG offers the wrong categories in the
// Add-vendor form while the list beside it looks correct.
test("the board scopes both the list and the catalog by its contract's side", () => {
  assert.match(board, /listProcurementVendors\(\{[^}]*\bside\b/s, "the vendor list read must carry the side");
  assert.match(board, /listProcurementVendorCatalog\(\{\s*side\s*\}\)/, "the catalog read must carry the side");
  assert.match(board, /function sideFromContract/, "the side must be read out of the page contract");
});
