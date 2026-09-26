import { FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /counts/milk-preparation: header + export, the park filter, four KPI cards, the preparation table card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <FilterCardSkeleton fields={[200]} />
      <KpiRowSkeleton count={4} />
      {/* Contract table "milk-preparation": 10 columns. */}
      <TableSkeleton columns={10} rows={10} headerAction />
    </PageSkeleton>
  );
}
