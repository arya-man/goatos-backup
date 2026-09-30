import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { SalesFarmTabsSkeleton, SalesFarmValueBodySkeleton } from "@/features/procurement/sales-skeletons";

/** /sales/farm-value: header, farm tabs, then the valuation panel's own twin (also its UrlSuspense fallback). */
export default function Loading() {
  return (
    <PageSkeleton root="page-root">
      <PageHeaderSkeleton />
      <SalesFarmTabsSkeleton />
      <SalesFarmValueBodySkeleton />
    </PageSkeleton>
  );
}
