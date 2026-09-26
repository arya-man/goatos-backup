import { FilterCardSkeleton, KpiRowSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /feed/packing: header, the date / park filter card, the two-card summary, the packing worklist card. */
export default function Loading() {
  return (
    <PageSkeleton className="feed-packing-page">
      <PageHeaderSkeleton />
      <FilterCardSkeleton fields={[200, 200]} />
      <OptionalSkeleton>
        <KpiRowSkeleton count={2} />
      </OptionalSkeleton>
      <TableSkeleton columns={6} rows={10} subheader />
    </PageSkeleton>
  );
}
