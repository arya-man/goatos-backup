import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /vaccination/care-coverage: header, the coverage matrix card (legend, park / pen filters, pen x category table, pager). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <TableSkeleton columns={6} rows={25} headerAction toolbar={<FilterCardSkeleton inCard fields={[200, 200]} />} />
    </PageSkeleton>
  );
}
