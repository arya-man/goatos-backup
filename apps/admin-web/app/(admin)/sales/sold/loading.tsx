import { PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { SalesFarmTabsSkeleton, SalesSoldLedgerSkeleton, SalesSoldOverviewSkeleton } from "@/features/procurement/sales-skeletons";
import { SALES_DEFAULT_LIMIT } from "@/features/procurement/sales-layout";

/** /sales/sold: header, farm tabs, then the overview and ledger panels' own twins (also their UrlSuspense fallbacks). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <SalesFarmTabsSkeleton />
      <SalesSoldOverviewSkeleton />
      <SalesSoldLedgerSkeleton limit={SALES_DEFAULT_LIMIT} />
    </PageSkeleton>
  );
}
