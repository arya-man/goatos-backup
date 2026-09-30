import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { SalesBuyerAnalyticsBodySkeleton, SalesFarmTabsSkeleton } from "@/features/procurement/sales-skeletons";
import { SALES_DEFAULT_LIMIT } from "@/features/procurement/sales-layout";

/** /sales/buyer-analytics: header, farm tabs, then the buyer panel's own twin (also its UrlSuspense fallback). */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton />
      <SalesFarmTabsSkeleton />
      <SalesBuyerAnalyticsBodySkeleton limit={SALES_DEFAULT_LIMIT} />
    </PageSkeleton>
  );
}
