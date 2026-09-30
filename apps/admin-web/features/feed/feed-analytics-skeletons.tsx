import { ChartCardSkeleton, ChipSkeleton, ControlRowSkeleton, FieldSkeleton, KpiRowSkeleton, StackSkeleton, TabsSkeleton, type KpiShape } from "@/components/app/skeletons";

// Shared by /feed/analytics loading.tsx and the page's tab-strip + Overview panel fallbacks, so the
// skeleton is the page (guard: feed-analytics-loading-mirror).

/** The page's tabs (the contract's feed_analytics_tabs narrow them) and its window chips. */
export const FEED_ANALYTICS_TABS = ["overview", "items", "peranimal", "experiment", "execution", "followup"] as const;
export const FEED_ANALYTICS_RANGES = ["30", "61", "92"] as const;
/** The Consumption view toggle (General / Status-wise segment) on the right of the chips row. */
export const FEED_ANALYTICS_VIEW_TOGGLE_WIDTH = 189;

/** The Overview KPI tiles' Grid item sizes: the three 7-day (App) tiles fill one row from md, the two
 *  course tiles fill the next (md 6 each), so no row leaves an empty slot and each row holds one widget
 *  kind (J2 P1-8; guard kpi-row-one-kind). */
export const FEED_ANALYTICS_KPI_SIZES = [
  { xs: 12, sm: 6, md: 4 },
  { xs: 12, sm: 6, md: 4 },
  { xs: 12, sm: 12, md: 4 },
  { xs: 12, sm: 6, md: 6 },
  { xs: 12, sm: 6, md: 6 },
];
/** Overview tiles: three with a weekly trend (ecommerce card), two without (course card); all captioned. */
export const FEED_ANALYTICS_KPI_SHAPES: KpiShape[] = [
  { spark: true, trend: true, hint: true },
  { spark: true, trend: true, hint: true },
  { spark: true, trend: true, hint: true },
  { hint: true },
  { hint: true },
];
/** The Overview charts' plot height (template chart card body). */
export const FEED_ANALYTICS_CHART_HEIGHT = 320;

/** The tab strip and, under it, the window chips with the Consumption view toggle on the right. */
export function FeedAnalyticsStripSkeleton() {
  return (
    <StackSkeleton spacing={2}>
      <TabsSkeleton count={FEED_ANALYTICS_TABS.length} />
      {/* One wrapping row, as the page's chip group: the chips, then the toggle (its own line on a phone). */}
      <ControlRowSkeleton>
        {FEED_ANALYTICS_RANGES.map((r) => (
          <ChipSkeleton key={r} width={72} />
        ))}
        <FieldSkeleton width={FEED_ANALYTICS_VIEW_TOGGLE_WIDTH} />
      </ControlRowSkeleton>
    </StackSkeleton>
  );
}

/** The Overview panel: the KPI deck, the consumption chart card (legend), the spend chart card (select). */
export function FeedAnalyticsOverviewSkeleton() {
  return (
    <StackSkeleton>
      <KpiRowSkeleton count={FEED_ANALYTICS_KPI_SHAPES.length} shapes={FEED_ANALYTICS_KPI_SHAPES} sizes={FEED_ANALYTICS_KPI_SIZES} />
      <ChartCardSkeleton height={FEED_ANALYTICS_CHART_HEIGHT} legend />
      <ChartCardSkeleton height={FEED_ANALYTICS_CHART_HEIGHT} subheader action />
    </StackSkeleton>
  );
}
