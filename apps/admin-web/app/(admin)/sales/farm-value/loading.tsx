import { ChartCardSkeleton, DetailCardSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /sales/farm-value: header, farm chips, then the page's template grid 1:1 (same Grid sizes, cards, order). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={3} variant="pill" />} />
      <StackSkeleton>
        <KpiRowSkeleton count={3} icon />
        <GridSkeleton
          items={[
            { size: { xs: 12, md: 6, lg: 5 }, node: <ChartCardSkeleton height={460} legend /> },
            { size: { xs: 12, md: 6, lg: 7 }, node: <DetailCardSkeleton rows={8} /> },
          ]}
        />
      </StackSkeleton>
  
    </PageSkeleton>
  );
}
