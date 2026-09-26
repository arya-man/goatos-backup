import { FilterCardSkeleton, GridSkeleton, KpiCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /counts/herd: header + actions, eight EcommerceWidgetSummary cards (four a row), the register table card (search + filters, cursor pager). */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton actions={2} />
      <GridSkeleton items={Array.from({ length: 8 }, () => ({ size: { xs: 12, sm: 6, md: 3 }, node: <KpiCardSkeleton hint /> }))} />
      {/* Contract table "herd-register": 11 columns, no checkbox, no action column. */}
      <TableSkeleton columns={11} rows={10} subheader toolbar={<FilterCardSkeleton inCard fields={["search", 120]} />} />
    </PageSkeleton>
  );
}
