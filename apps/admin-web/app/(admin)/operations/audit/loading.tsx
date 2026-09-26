import { ControlsCardSkeleton, BlockSkeleton, FilterCardSkeleton, GridSkeleton, KpiRowSkeleton, ListCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { PAGE_SIZE } from "@/features/operations-audit/audit-layout";

/** /operations/audit: header, four KPI cards, domain filter, status tabs + search card, span list beside the activity table, advanced filters. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={4} />
      <FilterCardSkeleton fields={[240]} />
      <ControlsCardSkeleton tabs={<TabsSkeleton count={4} counts />} toolbar={<FilterCardSkeleton inCard fields={["search", 140]} />} />
      <GridSkeleton
        items={[
          { size: { xs: 12, lg: 3 }, node: <ListCardSkeleton rows={4} /> },
          { size: { xs: 12, lg: 9 }, node: <TableSkeleton columns={7} rows={PAGE_SIZE} /> },
        ]}
      />
      <BlockSkeleton height={68} />
    </PageSkeleton>
  );
}
