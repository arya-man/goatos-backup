import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/** /procurement/source-entry: header + New load with the status tabs, the loads table card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} tabs={<TabsSkeleton count={5} />} />
      {/* Contract table "source-loads": 9 columns. */}
      <TableSkeleton columns={9} rows={10} toolbar={<FilterCardSkeleton inCard fields={["search", 120]} small />} />
    </PageSkeleton>
  );
}
