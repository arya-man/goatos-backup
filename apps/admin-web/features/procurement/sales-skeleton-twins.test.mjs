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

// REVIEW-44 O70/O71: the twins take the page's layout from the same constants the pages use.
test("pages and twins share sales-layout.ts (Grid sizes, page sizes); fallbacks pass the URL limit", () => {
  const twins = read("./sales-skeletons.tsx");
  assert.match(twins, /from "\.\/sales-layout"/);
  assert.doesNotMatch(twins, /size: \{ xs:|rows=\{25\}/, "no retyped Grid size or page size in the twins");
  assert.match(twins, /GROUPED_COLUMNS_HEIGHT/, "the load charts use GroupedColumns' own plot height");
  for (const { page } of PAGES) {
    if (page === "./market-analytics.tsx") continue;
    // Load wise lays its Grid out in LoadwiseSection.
    const file = page === "./sales-loads.tsx" ? "./loadwise-section.tsx" : page;
    assert.match(read(file), /from "\.\/sales-layout"/, `${file} imports its Grid sizes from sales-layout`);
    assert.doesNotMatch(read(file), /size=\{\{ xs: 12, (lg: [48]|sm: 6|md: [346])/, `${file}: Grid sizes come from SALES_GRID`);
  }
  assert.match(read("./sales-sold.tsx"), /fallback=\{<SalesSoldLedgerSkeleton limit=\{ledgerLimit\(sp, pageContract\)\.limit\} \/>\}/);
  assert.match(read("./sales-buyer-analytics.tsx"), /fallback=\{<SalesBuyerAnalyticsBodySkeleton limit=\{buyerLimit\(sp, pageContract\)\.limit\} \/>\}/);
  assert.match(read("./sales-farm-born.tsx"), /fallback=\{<SalesFarmBornBodySkeleton limit=\{limit\} \/>\}/);
  for (const route of ["sales/sold", "sales/buyer-analytics"]) assert.match(loading(route), /limit=\{SALES_DEFAULT_LIMIT\}/, route);
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
