import { FilterCardSkeleton, GridSkeleton, KpiCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/** /counts/herd: header + actions, three KPI tiles, the register card (status tabs with counts, search + filters, lead-cell table, cursor pager). */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton actions={2} />
      <GridSkeleton items={Array.from({ length: 3 }, () => ({ size: { xs: 12, sm: 4 }, node: <KpiCardSkeleton hint /> }))} />
      {/* Contract table "herd-register": 11 labels, the first three folded into the avatar lead cell. */}
      <TableSkeleton columns={9} rows={10} header={false} tabs={<TabsSkeleton count={5} counts />} toolbar={<FilterCardSkeleton inCard fields={["search", 120]} />} />
    </PageSkeleton>
  );
}
