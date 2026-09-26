import { ControlsCardSkeleton, FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/** /operations/dlq: header + Open audit, four KPI cards, status tabs + search card, the events table card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={4} />
      <ControlsCardSkeleton tabs={<TabsSkeleton count={3} counts />} toolbar={<FilterCardSkeleton inCard fields={["search", 200, 200]} actions={1} />} />
      {/* Contract table "dlq-events": 6 columns, one read of 100 rows, no pager. */}
      <TableSkeleton columns={6} rows={10} pager={false} />
    </PageSkeleton>
  );
}
