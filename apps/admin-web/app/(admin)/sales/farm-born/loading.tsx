import { FilterCardSkeleton, GridSkeleton, HeadingSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /sales/farm-born: header, farm toggle, the staged filter card, then the sections: KPI deck, breakdown cards, pen and sold tables. */
export default function Loading() {
  return (
    <PageSkeleton className="sales-farm-born-page">
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={2} variant="pill" />} />
      <FilterCardSkeleton fields={[260, 160, 160, 160, 160, 160]} actions={1} />
      <StackSkeleton>
        <KpiRowSkeleton count={4} hero hint />
        <HeadingSkeleton />
        <GridSkeleton
          items={[
            { size: { xs: 12, md: 6 }, node: <TableSkeleton columns={6} rows={8} pager={false} /> },
            { size: { xs: 12, md: 6 }, node: <TableSkeleton columns={6} rows={8} pager={false} /> },
          ]}
        />
      </StackSkeleton>
    </PageSkeleton>
  );
}
