import { FilterCardSkeleton, KpiRowSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /feed/direction: header, the date / park / pen / session filter card, the two-card day summary, the direction table card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <FilterCardSkeleton fields={[200, 200, 200, 200]} />
      <OptionalSkeleton>
        <KpiRowSkeleton count={2} icon hint />
      </OptionalSkeleton>
      <TableSkeleton columns={6} rows={10} subheader />
    </PageSkeleton>
  );
}
