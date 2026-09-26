import { GridSkeleton, KpiRowSkeleton, ListCardSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /workflows: header, four KPI cards, the domain tabs, the catalogue beside the chain map. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <KpiRowSkeleton count={4} hint />
      <ToolbarSkeleton left={<TabsSkeleton count={1} counts />} />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 5 }, node: <ListCardSkeleton rows={10} avatar={false} /> },
          { size: { xs: 12, md: 7 }, node: <ListCardSkeleton rows={8} /> },
        ]}
      />
    </PageSkeleton>
  );
}
