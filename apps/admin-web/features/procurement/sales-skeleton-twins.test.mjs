import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: sales-skeleton-twins (SK2). Every streamed Sales / vendor panel has ONE loading twin in
// features/procurement/{sales,vendor}-skeletons.tsx, used both as the panel's UrlSuspense fallback and
// by the route's loading.tsx, so a navigation, a direct load and an in-page farm / tab / filter switch
// paint the same blocks the panel drops into (skeleton IoU >= 0.8 at 1440 and 390, r2-visual-audit).
const read = (file) => readFileSync(new URL(file, import.meta.url), "utf8");
const loading = (route) => read(`../../app/(admin)/${route}/loading.tsx`);

const PAGES = [
  { page: "./sales-sold.tsx", route: "sales/sold", twins: ["SalesSoldOverviewSkeleton", "SalesSoldLedgerSkeleton"] },
  { page: "./sales-farm-value.tsx", route: "sales/farm-value", twins: ["SalesFarmValueBodySkeleton"] },
  { page: "./sales-loads.tsx", route: "sales/loads", twins: ["SalesLoadsBodySkeleton"] },
  { page: "./sales-farm-born.tsx", route: "sales/farm-born", twins: ["SalesFarmBornBodySkeleton"] },
  { page: "./sales-buyer-analytics.tsx", route: "sales/buyer-analytics", twins: ["SalesBuyerAnalyticsBodySkeleton"] },
  { page: "./market-analytics.tsx", route: "sales/market-analytics", twins: ["SalesMarketKpisSkeleton", "SalesMarketPanelsSkeleton"] },
];

test("each Sales panel's UrlSuspense fallback is the twin its loading.tsx renders", () => {
  for (const { page, route, twins } of PAGES) {
    const src = read(page);
    assert.doesNotMatch(src, /PanelSkeleton/, `${page}: the generic PanelSkeleton is not a twin of this panel`);
    for (const twin of twins) {
      assert.match(src, new RegExp(`fallback=\\{<${twin}[ />]`), `${page} falls back to ${twin}`);
      assert.match(loading(route), new RegExp(`<${twin}[ />]`), `${route}/loading.tsx renders ${twin}`);
    }
  }
});

test("the farm tabs placeholder has one tab per park the shell offers (+ All farms)", () => {
  const twins = read("./sales-skeletons.tsx");
  assert.match(twins, /useShellParks\(\)/);
  for (const route of ["sales/sold", "sales/farm-value", "sales/loads", "sales/farm-born", "sales/buyer-analytics"]) {
    assert.match(loading(route), /<SalesFarmTabsSkeleton \/>/, route);
  }
  assert.match(read("../../components/mesha-shell.tsx"), /<ShellParksContext\.Provider value=\{parks\}>/);
});

test("both vendor routes render the VendorBoardPage twin; the rows fall back to its rows", () => {
  assert.match(read("./vendor-board.tsx"), /fallback=\{<VendorRowsSkeleton /);
  for (const route of ["procurement/vendors", "sales/vendors"]) assert.match(loading(route), /<VendorBoardSkeleton \/>/, route);
});
