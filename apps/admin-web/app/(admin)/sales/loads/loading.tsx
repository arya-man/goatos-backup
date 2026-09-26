import { ChartCardSkeleton, DetailCardSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /sales/loads: header, farm chips, then the page's template grid 1:1 (same Grid sizes, cards, order). */
export default function Loading() {
  return (
    <PageSkeleton className="sales-loads-page">
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={3} variant="pill" />} />
      <StackSkeleton>
        <KpiRowSkeleton count={4} icon hint />
        <GridSkeleton
          items={[
            { size: { xs: 12, lg: 8 }, node: <ChartCardSkeleton height={340} legend /> },
            { size: { xs: 12, lg: 4 }, node: <DetailCardSkeleton rows={3} /> },
          ]}
        />
        <ChartCardSkeleton height={340} legend />
        <TableSkeleton columns={12} rows={6} />
      </StackSkeleton>
  
    </PageSkeleton>
  );
}
