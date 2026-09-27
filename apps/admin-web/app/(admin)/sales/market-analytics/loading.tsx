import { PageHeaderSkeleton, PageSkeleton, StackSkeleton } from "@/components/app/skeletons";
import { MARKET_WINDOWS } from "@/features/procurement/market-analytics-layout";
import { SalesMarketKpisSkeleton, SalesMarketPanelsSkeleton, SalesMarketWindowsSkeleton } from "@/features/procurement/sales-skeletons";

/** /sales/market-analytics: header, then the page's Stack — KPI deck, window pills, the price panels (the same twins the page's UrlSuspense fallbacks use). */
export default function Loading() {
  return (
    <PageSkeleton className="market-analytics-page">
      <PageHeaderSkeleton />
      <StackSkeleton>
        <SalesMarketKpisSkeleton />
        <SalesMarketWindowsSkeleton count={MARKET_WINDOWS.length} />
        <SalesMarketPanelsSkeleton />
      </StackSkeleton>
    </PageSkeleton>
  );
}
