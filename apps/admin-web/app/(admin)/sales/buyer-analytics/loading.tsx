import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { SalesBuyerAnalyticsBodySkeleton, SalesFarmTabsSkeleton } from "@/features/procurement/sales-skeletons";

/** /sales/buyer-analytics: header, farm tabs, then the buyer panel's own twin (also its UrlSuspense fallback). */
export default function Loading() {
  return (
    <PageSkeleton className="sales-buyer-analytics-page">
      <PageHeaderSkeleton />
      <SalesFarmTabsSkeleton />
      <SalesBuyerAnalyticsBodySkeleton />
    </PageSkeleton>
  );
}
