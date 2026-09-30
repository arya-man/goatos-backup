import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { BD_FILTER_FIELDS, BD_TABLE } from "@/features/counts/counts-layout";
import { BreakdownChartsSkeleton, BreakdownKpiSkeleton } from "@/features/counts/counts-skeletons";

/**
 * /counts/breakdown: header, the SAME KPI deck and chart grid its UrlSuspense panels show, and between
 * them the pen table card (filters in the card, pager).
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton crumbLink={false} />
      <BreakdownKpiSkeleton />
      <TableSkeleton columns={BD_TABLE.columns} rows={BD_TABLE.rows} header={false} toolbar={<FilterCardSkeleton inCard fields={BD_FILTER_FIELDS} />} />
      <BreakdownChartsSkeleton />
    </PageSkeleton>
  );
}
