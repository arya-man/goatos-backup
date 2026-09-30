import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { SalesFarmBornBodySkeleton, SalesFarmTabsSkeleton } from "@/features/procurement/sales-skeletons";

/**
 * /sales/farm-born: header (no subtitle), farm tabs, the WorklistFilters bar (fields from md, one
 * Filters button below; the staged Apply sits with the fields), then the sections' own twin (also
 * their UrlSuspense fallback).
 */
export default function Loading() {
  return (
    <PageSkeleton className="sales-farm-born-page">
      <PageHeaderSkeleton />
      <SalesFarmTabsSkeleton />
      <FilterCardSkeleton fields={[260, 160, 160, 160, 160, 160, 88]} fold />
      <SalesFarmBornBodySkeleton />
    </PageSkeleton>
  );
}
