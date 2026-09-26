import { FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /counts/herd: header + actions, eight summary cards, the register table card (search + page size, cursor pager). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={2} />
      <KpiRowSkeleton count={8} hint />
      {/* Contract table "herd-register": 11 columns, no checkbox, no action column. */}
      <TableSkeleton columns={11} rows={10} subheader toolbar={<FilterCardSkeleton inCard fields={["search", 120]} />} />
    </PageSkeleton>
  );
}
