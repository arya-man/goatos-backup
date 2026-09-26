import { FilterCardSkeleton, HeadingSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { HERD_SIGNALS_TABS, LIMIT_DEFAULT } from "@/features/herd-signals/params";

/** /herd-signals (route loading AND the page's Suspense fallback): header with the six signal tabs, the filter card, the eight KPI filters, the live table card. */
export default function Loading() {
  return (
    <PageSkeleton root="herd-signals-page">
      <PageHeaderSkeleton actionWidths={[120]} tabs={<TabsSkeleton count={HERD_SIGNALS_TABS.length} counts />} />
      <FilterCardSkeleton fields={["search", 180, 180, 120]} />
      {/* KPI_DEFS in herd-signals-kpis.tsx: eight KPI filter cards. */}
      <KpiRowSkeleton count={8} />
      <HeadingSkeleton variant="caption" width={320} />
      <TableSkeleton columns={21} rows={LIMIT_DEFAULT} />
    </PageSkeleton>
  );
}
