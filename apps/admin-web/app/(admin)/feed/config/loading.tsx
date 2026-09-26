import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /feed/config: header, the ration grid card (five filters, pager), then the experiment card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      {/* Contract table "ration-grid": 6 columns. */}
      <TableSkeleton columns={6} rows={10} toolbar={<FilterCardSkeleton inCard fields={[180, 180, 180, 180, 180]} />} />
      {/* Contract table "experiment-config": 7 columns + the edit column. */}
      <TableSkeleton columns={8} rows={10} toolbar={<FilterCardSkeleton inCard fields={[180, 180, 180, 180, 180]} />} />
    </PageSkeleton>
  );
}
