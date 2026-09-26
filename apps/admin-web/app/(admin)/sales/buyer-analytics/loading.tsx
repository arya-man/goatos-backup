import { DetailCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /sales/buyer-analytics: header, farm toggle, four KPI cards, the gauges card, the buyers table card. */
export default function Loading() {
  return (
    <PageSkeleton className="sales-buyer-analytics-page">
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={2} variant="pill" />} />
      <KpiRowSkeleton count={4} hero hint />
      <DetailCardSkeleton rows={4} columns={2} />
      <TableSkeleton columns={9} rows={25} />
    </PageSkeleton>
  );
}
