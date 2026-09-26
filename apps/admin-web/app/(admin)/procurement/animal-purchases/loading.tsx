import { KpiRowSkeleton, ListCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/** /procurement/animal-purchases: header, four KPI cards, the loads table card, the animals card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <KpiRowSkeleton count={4} />
      {/* Contract table "animal-purchase-loads": 9 columns. */}
      <TableSkeleton columns={9} rows={5} headerAction={<TabsSkeleton count={2} variant="pill" />} />
      <ListCardSkeleton rows={6} />
    </PageSkeleton>
  );
}
