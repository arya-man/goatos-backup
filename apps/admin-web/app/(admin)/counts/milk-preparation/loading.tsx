import { FilterCardSkeleton, GridSkeleton, KpiCardSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /counts/milk-preparation: header + export, four EcommerceWidgetSummary cards, the farm-state InvoiceAnalytic strip, the list card (park toolbar, table, pager). */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton actions={1} />
      <GridSkeleton items={Array.from({ length: 4 }, () => ({ size: { xs: 12, sm: 6, md: 3 }, node: <KpiCardSkeleton /> }))} />
      <StatStripSkeleton count={4} meta />
      {/* Contract table "milk-preparation": 10 columns. */}
      <TableSkeleton columns={10} rows={10} subheader toolbar={<FilterCardSkeleton inCard fields={[200]} />} />
    </PageSkeleton>
  );
}
