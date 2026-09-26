import { FilterCardSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /sales/farm-born: header, farm chips, then the page's template grid 1:1 (same Grid sizes, cards, order). */
export default function Loading() {
  return (
    <PageSkeleton className="sales-farm-born-page">
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={3} variant="pill" />} />
      <StackSkeleton>
        <FilterCardSkeleton fields={[260, 160, 160, 160, 160, 160]} actions={1} />
        <KpiRowSkeleton count={4} icon hint />
        <GridSkeleton
          items={[
            { size: { xs: 12, md: 6 }, node: <TableSkeleton columns={6} rows={8} pager={false} /> },
            { size: { xs: 12, md: 6 }, node: <TableSkeleton columns={6} rows={4} pager={false} /> },
          ]}
        />
        <TableSkeleton columns={5} rows={10} />
      </StackSkeleton>
  
    </PageSkeleton>
  );
}
