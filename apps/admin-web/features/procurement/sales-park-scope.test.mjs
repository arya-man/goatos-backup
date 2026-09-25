import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { resolveSalesParkScope, salesPageHref, salesParkPatch } from "./sales-park-scope.ts";
import { parseScope, scopeHref } from "../../lib/scope.ts";

// ONE park filter across every Sales read page (defect 2026-09-25). Repro: on /sales/sold pick CPT,
// then click Buyer analytics in the sidebar -- it showed All farms, because Sold wrote `?farm=CPT`
// and the sidebar carries only the shell's scope (`scope_mode` / `park`). Load wise read `park`,
// so a CPT picked there was lost on Summary the same way.

const CBE = { id: "11111111-1111-4111-8111-111111111111", code: "CBE" };
const CPT = { id: "22222222-2222-4222-8222-222222222222", code: "CPT" };
const PARKS = [CBE, CPT];
const FARMS = ["all", "CBE", "CPT"];

/** The URL a chip builds, read back as the search params the next page receives. */
function params(href) {
  return Object.fromEntries(new URL(href, "http://x").searchParams);
}

/** What the sidebar links to from a page whose URL is `href` (mesha-shell navHref). */
function sidebarLink(href, leafPath) {
  return scopeHref(leafPath, parseScope(params(href)));
}

test("a park chosen on one Sales page is carried by the sidebar to every other", () => {
  // The CPT chip on Summary.
  const onSold = salesPageHref("/sales/sold", {}, salesParkPatch(CPT.id));
  for (const leaf of ["/sales/buyer-analytics", "/sales/farm-value", "/sales/loads", "/sales/farm-born", "/sales/sold"]) {
    const next = sidebarLink(onSold, leaf);
    const scope = resolveSalesParkScope(params(next), PARKS, FARMS, leaf);
    assert.equal(scope.parkId, CPT.id, `${leaf} lost the park`);
    assert.equal(scope.farm, "CPT", `${leaf} lost the farm code`);
    assert.equal(scope.redirectTo, null);
  }
});

test("every farms clears the park the shell's way", () => {
  const all = salesPageHref("/sales/sold", { scope_mode: "park", park: CPT.id, limit: "50" }, salesParkPatch(""));
  assert.deepEqual(params(all), { scope_mode: "company", limit: "50" });
  assert.equal(resolveSalesParkScope(params(all), PARKS, FARMS, "/sales/sold").farm, "all");
});

test("a chip keeps the page's other filters and drops what the switch invalidates", () => {
  const href = salesPageHref(
    "/sales/farm-born",
    { breed: "Boer", pen: "pen-1", pen_offset: "10", offset: "25", from: "2026-09-01", to: "2026-09-20" },
    { pen: null, offset: null, pen_offset: null, ...salesParkPatch(CBE.id) },
  );
  assert.deepEqual(params(href), {
    breed: "Boer",
    from: "2026-09-01",
    to: "2026-09-20",
    scope_mode: "park",
    park: CBE.id,
  });
});

test("a bookmarked ?farm=CPT still lands on CPT, redirected onto the shell's park", () => {
  const scope = resolveSalesParkScope({ farm: "CPT", limit: "50" }, PARKS, FARMS, "/sales/buyer-analytics");
  assert.equal(scope.parkId, CPT.id);
  assert.equal(scope.farm, "CPT");
  assert.deepEqual(params(scope.redirectTo), { limit: "50", scope_mode: "park", park: CPT.id });
  assert.ok(scope.redirectTo.startsWith("/sales/buyer-analytics?"));
  // ?farm=all is every farm, canonicalised too.
  const all = resolveSalesParkScope({ farm: "all" }, PARKS, FARMS, "/sales/sold");
  assert.equal(all.parkId, "");
  assert.equal(all.redirectTo, "/sales/sold?scope_mode=company");
});

test("park wins over a stale farm, and the stale farm is dropped", () => {
  const scope = resolveSalesParkScope({ park: CBE.id, farm: "CPT" }, PARKS, FARMS, "/sales/sold");
  assert.equal(scope.farm, "CBE");
  assert.deepEqual(params(scope.redirectTo), { park: CBE.id, scope_mode: "park" });
});

test("a park the caller cannot see, or one no farm chip names, reads as every farm", () => {
  assert.deepEqual(resolveSalesParkScope({ park: "not-a-park" }, PARKS, FARMS, "/sales/sold"), {
    parkId: "",
    farm: "all",
    redirectTo: null,
  });
  // A caller scoped to CPT alone: the CBE code has no park for them.
  const cbeOnly = resolveSalesParkScope({ farm: "CBE" }, [CPT], FARMS, "/sales/sold");
  assert.equal(cbeOnly.parkId, "");
  assert.equal(cbeOnly.farm, "all");
  // The park's code is not one the page serves (a hand-edited or a future park).
  assert.equal(resolveSalesParkScope({ park: CPT.id }, PARKS, ["all", "CBE"], "/sales/sold").farm, "all");
  // The URL's own "all" is every farm, not a lookup.
  assert.equal(resolveSalesParkScope({ park: "all" }, PARKS, FARMS, "/sales/sold").parkId, "");
});

test("every Sales read page reads the one park and renders the same chips", () => {
  for (const file of [
    "sales-sold.tsx",
    "sales-farm-value.tsx",
    "sales-buyer-analytics.tsx",
    "sales-loads.tsx",
    "sales-farm-born.tsx",
  ]) {
    const source = readFileSync(new URL(`./${file}`, import.meta.url), "utf8");
    assert.match(source, /readSalesParkScope\(sp, pageContract, PAGE_PATH\)/, `${file} does not read the one park`);
    assert.match(source, /<SalesFarmToggle[\s\S]*?searchParams=\{sp\}/, `${file} does not render the farm chips`);
    assert.doesNotMatch(source, /one\(sp, "farm"\)|one\(sp, "park"\)/, `${file} reads a park parameter of its own`);
    assert.doesNotMatch(source, /param: "park"/, `${file} renders a second park control`);
  }
});
