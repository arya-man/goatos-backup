import { ChartCardSkeleton, DetailCardSkeleton, GridSkeleton, KpiCardSkeleton, ListCardSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

function Kpi2x2Skeleton({ spark }: { spark: boolean }) {
  return <GridSkeleton items={Array.from({ length: 4 }, () => ({ size: { xs: 12, sm: 6 }, node: <KpiCardSkeleton spark={spark} trend={spark} /> }))} />;
}

/** /sales/sold: header, farm chips, then the page's template grid 1:1 (same Grid sizes, cards, order). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={3} variant="pill" />} />
      <StackSkeleton>
        <GridSkeleton
          items={[
            { size: { xs: 12, lg: 8 }, node: <Kpi2x2Skeleton spark /> },
            { size: { xs: 12, lg: 4 }, node: <DetailCardSkeleton rows={4} /> },
            { size: { xs: 12, lg: 8 }, node: <ChartCardSkeleton height={320} action legend /> },
            { size: { xs: 12, lg: 4 }, node: <ListCardSkeleton rows={6} /> },
          ]}
        />
        <TableSkeleton columns={7} rows={10} />
        <TableSkeleton columns={9} rows={10} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
