import { BlockSkeleton, FilterCardSkeleton, GridSkeleton, ListCardSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { PAGE_SIZE } from "@/features/operations-audit/audit-layout";

/** /operations/audit: header + Export, the InvoiceAnalytic strip, the list card (tabs, toolbar, table, pager), operators beside the advanced filters. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <StatStripSkeleton count={4} meta />
      <TableSkeleton
        header={false}
        columns={7}
        rows={PAGE_SIZE}
        tabs={<TabsSkeleton count={4} counts />}
        toolbar={<FilterCardSkeleton inCard fields={[200, "search", 140]} />}
      />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 5 }, node: <ListCardSkeleton rows={4} /> },
          { size: { xs: 12, md: 7 }, node: <BlockSkeleton height={68} /> },
        ]}
      />
    </PageSkeleton>
  );
}
