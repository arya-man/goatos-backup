import { ChartCardSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /counts/mortality: header, date range, six KPI cards, the monthly stacked chart, then the two-up rate cards. */
export default function Loading() {
  return (
    <PageSkeleton root="kit-enter pagegrid mortality-page">
      <PageHeaderSkeleton />
      <ToolbarSkeleton fields={[280]} />
      <KpiRowSkeleton count={6} shapes={[{ spark: true }, { spark: true }, { spark: true }, {}, {}, {}]} />
      <ChartCardSkeleton height={300} action />
      <GridSkeleton
        items={[0, 1, 2, 3].map(() => ({ size: { xs: 12, lg: 6 }, node: <TableSkeleton columns={5} rows={5} pager={false} /> }))}
      />
    </PageSkeleton>
  );
}
