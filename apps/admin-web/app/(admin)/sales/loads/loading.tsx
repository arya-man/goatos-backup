import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { SalesFarmTabsSkeleton, SalesLoadsBodySkeleton } from "@/features/procurement/sales-skeletons";

/** /sales/loads: header, farm tabs, then the load-wise panel's own twin (also its UrlSuspense fallback). */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton />
      <SalesFarmTabsSkeleton />
      <SalesLoadsBodySkeleton />
    </PageSkeleton>
  );
}
