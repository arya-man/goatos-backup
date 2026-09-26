import { ChartCardSkeleton, GridSkeleton, KpiCardSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

function Kpi2x2Skeleton({ spark }: { spark: boolean }) {
  return <GridSkeleton items={Array.from({ length: 4 }, () => ({ size: { xs: 12, sm: 6 }, node: <KpiCardSkeleton spark={spark} trend={spark} /> }))} />;
}

/** /sales/buyer-analytics: header, farm chips, then the page's template grid 1:1 (same Grid sizes, cards, order). */
export default function Loading() {
  return (
    <PageSkeleton className="sales-buyer-analytics-page">
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={3} variant="pill" />} />
      <StackSkeleton>
        <GridSkeleton
          items={[
            { size: { xs: 12, lg: 8 }, node: <Kpi2x2Skeleton spark={false} /> },
            { size: { xs: 12, lg: 4 }, node: <ChartCardSkeleton height={240} legend /> },
          ]}
        />
        <TableSkeleton columns={9} rows={10} />
      </StackSkeleton>
  
    </PageSkeleton>
  );
}
