import { ChartCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";
import { MARKET_WINDOWS } from "@/features/procurement/market-analytics-layout";

/** /sales/market-analytics: header, four KPI cards, the window pills, the latest-readings card, the trend chart card. */
export default function Loading() {
  return (
    <PageSkeleton className="market-analytics-page">
      <PageHeaderSkeleton />
      <KpiRowSkeleton count={4} hero />
      <ToolbarSkeleton left={<TabsSkeleton count={MARKET_WINDOWS.length} variant="pill" />} />
      <TableSkeleton columns={5} rows={4} pager={false} />
      <ChartCardSkeleton height={{ xs: 240, md: 280 }} legend />
    </PageSkeleton>
  );
}
