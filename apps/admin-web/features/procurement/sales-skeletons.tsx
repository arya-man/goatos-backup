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

/** Template chart card of LoadwiseSection (`chartCard`): the chart's own height per breakpoint. */
const loadChart = (height: Record<string, number>) => <ChartCardSkeleton height={height} />;

/** /sales/loads `LoadwiseSection`: KPI row, animals-per-load chart beside the summary, four chart cards, the register. */
export function SalesLoadsBodySkeleton() {
  return (
    <StackSkeleton>
      <KpiRowSkeleton count={4} shapes={[{ hint: true, hintLines: { xs: 1, lg: 2 } }, { hint: true, hintLines: { xs: 1, lg: 2 } }, { hint: true }, { hint: true, hintLines: { xs: 3, lg: 5 } }]} />
      <GridSkeleton
        items={[
          { size: { xs: 12, lg: 8 }, node: loadChart({ xs: 491, lg: 391 }) },
          { size: { xs: 12, lg: 4 }, node: <DetailCardSkeleton rows={6} height={{ lg: "100%" }} /> },
        ]}
      />
      {loadChart({ xs: 491, lg: 391 })}
      {loadChart({ xs: 440, lg: 391 })}
      {loadChart({ xs: 389, lg: 391 })}
      {loadChart({ xs: 440, lg: 391 })}
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
                { size: { xs: 12, md: 4 }, node: <KpiCardSkeleton hint /> },
                { size: { xs: 12, md: 4 }, node: <KpiCardSkeleton hint /> },
                {
                  size: { xs: 12, md: 4 },
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
        { size: { xs: 12, md: 6, lg: 5 }, node: <ChartCardSkeleton height={{ xs: 410, md: 460 }} legend /> },
        // EcommerceSalesOverview progress rows (label, bar, caption: two detail rows each), stretched beside the chart from md.
        { size: { xs: 12, md: 6, lg: 7 }, node: <DetailCardSkeleton rows={14} height={{ md: "100%" }} /> },
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
  return <GridSkeleton fill items={Array.from({ length: 4 }, (_, i) => ({ size: { xs: 12, sm: 6 }, node: <KpiCardSkeleton hint hintLines={lines[i] ?? 1} /> }))} />;
}

/** /sales/sold `SoldOverviewPanel`: KPIs 2x2 beside sold-by-weight, the monthly chart beside price by breed, the buyers board. */
export function SalesSoldOverviewSkeleton() {
  return (
    <GridSkeleton
      items={[
        { size: { xs: 12, lg: 8 }, node: <Kpi2x2Skeleton lines={[1, 1, 1, { xs: 2, sm: 1 }]} /> },
        { size: { xs: 12, lg: 4 }, node: <DetailCardSkeleton rows={13} /> },
        { size: { xs: 12, lg: 8 }, node: <ChartCardSkeleton height={{ xs: 329, lg: 380 }} action legend /> },
        { size: { xs: 12, lg: 4 }, node: <ListCardSkeleton rows={7} /> },
        // RankedTableCard: ten avatar rows are as tall as ~14 text rows.
        { size: 12, node: <TableSkeleton columns={7} rows={14} headerAction /> },
      ]}
    />
  );
}

/** /sales/sold `SoldLedgerPanel`: the deals ledger card (`mt: 3` under the overview grid), one server page of rows. */
export function SalesSoldLedgerSkeleton() {
  return (
    // Layout-transparent like the page's UrlPanel, so the card's own mt is not reset as a direct
    // `.wrap > .screen > *` child.
    <Box sx={{ display: "contents" }}>
      <Box sx={{ mt: 3 }}>
        <TableSkeleton columns={9} rows={25} headerAction />
      </Box>
    </Box>
  );
}

/** /sales/buyer-analytics `BuyerAnalyticsPanel`: KPIs 2x2 beside the repeat-share ring, the buyer table card. */
export function SalesBuyerAnalyticsBodySkeleton() {
  return (
    <GridSkeleton
      items={[
        { size: { xs: 12, lg: 8 }, node: <Kpi2x2Skeleton /> },
        { size: { xs: 12, lg: 4 }, node: <ChartCardSkeleton height={403} /> },
        { size: 12, node: <TableSkeleton columns={9} rows={25} headerAction /> },
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
export function SalesFarmBornBodySkeleton({ limit = 25 }: { limit?: number }) {
  return (
    <Box>
      <KpiRowSkeleton count={4} size={{ xs: 12, sm: 6, md: 3 }} shapes={[{}, { hint: true }, { hint: true }, { hint: true }]} />
      <Box sx={{ mt: 3 }}>
        <GridSkeleton
          items={[
            { size: { xs: 12, md: 6 }, node: breakdown(10) },
            { size: { xs: 12, md: 6 }, node: <StackSkeleton>{breakdown(2)}{breakdown(2)}</StackSkeleton> },
            { size: 12, node: breakdown(13, true) },
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
