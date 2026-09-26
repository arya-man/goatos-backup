import { ChartCardSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /counts/mortality: header, date range, six EcommerceWidgetSummary cards, the stacked monthly chart, then the two-up rate cards. */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton />
      <ToolbarSkeleton fields={[300]} small={false} />
      <KpiRowSkeleton count={6} hint />
      <ChartCardSkeleton height={364} />
      <GridSkeleton
        items={[0, 1, 2, 3].map(() => ({ size: { xs: 12, lg: 6 }, node: <TableSkeleton columns={5} rows={5} pager={false} /> }))}
      />
    </PageSkeleton>
  );
}
