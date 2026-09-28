import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { COUNTS_HEADER, HERD_HEADER_LAYOUT, HERD_TABLE } from "@/features/counts/counts-layout";
import { HerdKpiSkeleton } from "@/features/counts/counts-skeletons";

/** /counts/herd: header + actions, the SAME KPI cards its UrlSuspense shows, the register card (status tabs with counts, search + filters, lead-cell table, cursor pager). */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton layout={HERD_HEADER_LAYOUT} crumbLink={false} crumbWidths={[...COUNTS_HEADER.herdCrumbWidths]} actionWidths={[...COUNTS_HEADER.herdActionWidths]} />
      <HerdKpiSkeleton />
      {/* Contract table "herd-register": 11 labels, the first three folded into the avatar lead cell. */}
      <TableSkeleton columns={HERD_TABLE.columns} rows={HERD_TABLE.rows} header={false} tabs={<TabsSkeleton count={HERD_TABLE.statusTabs} counts />} toolbar={<FilterCardSkeleton inCard fields={HERD_TABLE.searchFields} />} />
    </PageSkeleton>
  );
}
