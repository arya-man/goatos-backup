"use client";

// Loading twins of the Sales pages' streamed panels. Each is used twice: by the route's loading.tsx
// (under the header + farm tabs) and as the page's UrlSuspense fallback, so a direct load, a
// navigation and an in-page tab / farm switch all paint the SAME blocks the panel drops into.
// Composed only from the shared blocks (components/app/skeletons) on the panels' own Grid sizes.

import {
  ChartCardSkeleton,
  DetailCardSkeleton,
  GridSkeleton,
  KpiCardSkeleton,
  ListCardSkeleton,
  KpiRowSkeleton,
  SkeletonLine,
  StackSkeleton,
  TableSkeleton,
  FieldSkeleton,
  TabsSkeleton,
  ToolbarSkeleton,
} from "@/components/app/skeletons";
import type { KpiShape } from "@/components/app/skeletons";
import Box from "@mui/material/Box";
import { useShellParks } from "@/components/app/shell-parks";
import { GROUPED_COLUMNS_HEIGHT } from "@/components/grouped-columns";
import { FARM_BORN_PEN_PAGE_SIZE, SALES_CHART_TWIN, SALES_DEFAULT_LIMIT, SALES_GRID, SOLD_BUYERS_PAGE_SIZE } from "./sales-layout";

/** Template chart card of LoadwiseSection (`chartCard`): GroupedColumns' plot plus its legend rows. */
const loadChart = (i: number) => {
  const legend = SALES_CHART_TWIN.loadLegends[i];
  return <ChartCardSkeleton height={{ xs: GROUPED_COLUMNS_HEIGHT + legend.xs, lg: GROUPED_COLUMNS_HEIGHT + legend.lg }} />;
};

/** /sales/loads `LoadwiseSection`: KPI row, animals-per-load chart beside the summary, four chart cards, the register. */
export function SalesLoadsBodySkeleton() {
  return (
    <StackSkeleton>
      <KpiRowSkeleton count={4} shapes={[{ hint: true, hintLines: { xs: 1, lg: 2 } }, { hint: true, hintLines: { xs: 1, lg: 2 } }, { hint: true }, { hint: true, hintLines: { xs: 3, lg: 5 } }]} />
      <GridSkeleton
        items={[
          { size: SALES_GRID.main, node: loadChart(0) },
          { size: SALES_GRID.side, node: <DetailCardSkeleton rows={6} height={{ lg: "100%" }} /> },
        ]}
      />
      {loadChart(1)}
      {loadChart(2)}
      {loadChart(3)}
      {loadChart(4)}
      <TableSkeleton columns={12} rows={6} />
    </StackSkeleton>
  );
}

/** /sales/farm-value `FarmValueSections`: one Grid — the three valuation cards (Over 35 kg with its margin control), the categories chart beside the breakdown rows. */
export function SalesFarmValueBodySkeleton() {
  return (
    <GridSkeleton
      items={[
        {
          size: 12,
          node: (
            <GridSkeleton
              items={[
                { size: SALES_GRID.valueKpi, node: <KpiCardSkeleton hint /> },
                { size: SALES_GRID.valueKpi, node: <KpiCardSkeleton hint /> },
                {
                  size: SALES_GRID.valueKpi,
                  node: (
                    <StackSkeleton spacing={1.5}>
                      <KpiCardSkeleton hint hintLines={2} />
                      {/* SalesReadyToleranceControl: mt 1.5, the label row, then the slider + Apply row
                          (it wraps to two 44px rows on a phone). */}
                      <Box>
                        <SkeletonLine variant="body2" width="100%" sx={{ mt: 1.5, mb: 1 }} />
                        <FieldSkeleton grow height={{ xs: 77, md: 38 }} />
                      </Box>
                    </StackSkeleton>
                  ),
                },
              ]}
            />
          ),
        },
        { size: SALES_GRID.valueChart, node: <ChartCardSkeleton height={SALES_CHART_TWIN.valueCategories} legend /> },
        // EcommerceSalesOverview progress rows (label, bar, caption: two detail rows each), stretched beside the chart from md.
        { size: SALES_GRID.valueRows, node: <DetailCardSkeleton rows={14} height={{ md: "100%" }} /> },
      ]}
    />
  );
}

/**
 * Loading twin of `SalesFarmToggle`: the same pill tab strip with one tab for "All farms" plus one
 * per park the shell offers (the toggle offers a farm per park), each label as wide as its text.
 * Before the shell contract lands (a direct load) the park list is empty: one park is assumed.
 */
export function SalesFarmTabsSkeleton() {
  const parks = useShellParks();
  const codes = parks.length ? parks.map((park) => park.code ?? park.name) : ["CBE"];
  const widths = [64, ...codes.map((code) => Math.max(28, code.length * 9))];
  return <ToolbarSkeleton left={<TabsSkeleton count={widths.length} widths={widths} variant="pill" links />} />;
}

/** Four template CourseWidgetSummary cards two by two, filling the Grid item they sit in (Sold, Buyer analytics). */
function Kpi2x2Skeleton({ lines = [] }: { lines?: KpiShape["hintLines"][] }) {
  return <GridSkeleton fill items={Array.from({ length: 4 }, (_, i) => ({ size: SALES_GRID.half, node: <KpiCardSkeleton hint hintLines={lines[i] ?? 1} /> }))} />;
}

/** /sales/sold `SoldOverviewPanel`: KPIs 2x2 beside sold-by-weight, the monthly chart beside price by breed, the buyers board. */
export function SalesSoldOverviewSkeleton() {
  return (
    <GridSkeleton
      items={[
        { size: SALES_GRID.main, node: <Kpi2x2Skeleton lines={[1, 1, 1, { xs: 2, sm: 1 }]} /> },
        { size: SALES_GRID.side, node: <DetailCardSkeleton rows={13} /> },
        { size: SALES_GRID.main, node: <ChartCardSkeleton height={SALES_CHART_TWIN.soldMonthly} action legend /> },
        { size: SALES_GRID.side, node: <ListCardSkeleton rows={7} /> },
        // RankedTableCard: its avatar rows are ~1.4 text rows each.
        { size: 12, node: <TableSkeleton columns={7} rows={Math.round(SOLD_BUYERS_PAGE_SIZE * 1.4)} headerAction /> },
      ]}
    />
  );
}

/** /sales/sold `SoldLedgerPanel`: the deals ledger card (`mt: 3` under the overview grid), one server page of rows. */
export function SalesSoldLedgerSkeleton({ limit = SALES_DEFAULT_LIMIT }: { limit?: number }) {
  return (
    // Layout-transparent like the page's UrlPanel, so the card's own mt is not reset as a direct
    // `.wrap > .screen > *` child.
    <Box sx={{ display: "contents" }}>
      <Box sx={{ mt: 3 }}>
        <TableSkeleton columns={9} rows={limit} headerAction />
      </Box>
    </Box>
  );
}

/** /sales/buyer-analytics `BuyerAnalyticsPanel`: KPIs 2x2 beside the repeat-share ring, the buyer table card. */
export function SalesBuyerAnalyticsBodySkeleton({ limit = SALES_DEFAULT_LIMIT }: { limit?: number }) {
  return (
    <GridSkeleton
      items={[
        { size: SALES_GRID.main, node: <Kpi2x2Skeleton /> },
        { size: SALES_GRID.side, node: <ChartCardSkeleton height={SALES_CHART_TWIN.buyerRing} /> },
        { size: 12, node: <TableSkeleton columns={9} rows={limit} headerAction /> },
      ]}
    />
  );
}

/** A BreakdownCard (farm born): card header, the six-column bucket table, the pager when it pages. */
const breakdown = (rows: number, pager = false) => <TableSkeleton columns={6} rows={rows} pager={pager} />;

/**
 * /sales/farm-born `FarmBornSections` inside WorklistFilters' children block: the headline cards, the
 * breakdown grid (breed beside sex over stage, pens across) and the sold-animals card, each `mt: 3`.
 */
export function SalesFarmBornBodySkeleton({ limit = SALES_DEFAULT_LIMIT }: { limit?: number }) {
  return (
    <Box>
      <KpiRowSkeleton count={4} size={SALES_GRID.bornKpi} shapes={[{}, { hint: true }, { hint: true }, { hint: true }]} />
      <Box sx={{ mt: 3 }}>
        <GridSkeleton
          items={[
            { size: SALES_GRID.bornHalf, node: breakdown(10) },
            { size: SALES_GRID.bornHalf, node: <StackSkeleton>{breakdown(2)}{breakdown(2)}</StackSkeleton> },
            // The pen rows carry a caption line: ~1.3 text rows each.
            { size: 12, node: breakdown(Math.round(FARM_BORN_PEN_PAGE_SIZE * 1.3), true) },
          ]}
        />
      </Box>
      <Box sx={{ mt: 3 }}>
        <TableSkeleton columns={5} rows={Math.min(limit, 5)} headerAction />
      </Box>
    </Box>
  );
}

/** /sales/market-analytics KPI deck: cities, days, then latest survey and coverage with their sub-lines. */
export function SalesMarketKpisSkeleton() {
  return <KpiRowSkeleton count={4} shapes={[{}, {}, { hint: true }, { hint: true }]} />;
}

/** /sales/market-analytics window strip: SegmentTabs link pills, one per window. */
export function SalesMarketWindowsSkeleton({ count }: { count: number }) {
  return <ToolbarSkeleton left={<TabsSkeleton count={count} widths={[76, 76, 84, 76]} variant="pill" links />} />;
}

/** /sales/market-analytics price panels: the latest-readings table card and the trend chart card. */
export function SalesMarketPanelsSkeleton() {
  return (
    <StackSkeleton>
      <TableSkeleton columns={5} rows={6} pager={false} subheader headerAction />
      <ChartCardSkeleton height={320} subheader />
    </StackSkeleton>
  );
}
