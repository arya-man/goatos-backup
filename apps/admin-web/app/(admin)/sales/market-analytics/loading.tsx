import { ChartCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";
import { MARKET_WINDOWS } from "@/features/procurement/market-analytics-layout";

/** /sales/market-analytics: header, four KPI cards, the window pills, the latest-readings card, the trend chart card. */
export default function Loading() {
  return (
    <PageSkeleton className="market-analytics-page">
      <PageHeaderSkeleton />
      {/* The same shapes as the page's own in-place fallbacks (KpiWidget row, window pills, the
          latest-readings table card and the trend chart card), so nothing jumps on arrival. */}
      <KpiRowSkeleton count={4} hero />
      <ToolbarSkeleton left={<TabsSkeleton count={MARKET_WINDOWS.length} variant="pill" />} />
      <TableSkeleton columns={5} rows={6} pager={false} subheader headerAction />
      <ChartCardSkeleton height={320} subheader />
    </PageSkeleton>
  );
}
