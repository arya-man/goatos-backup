import { PageHeaderSkeleton, PageSkeleton, TableSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";
import { DEFAULT_LIMIT } from "@/features/procurement/feed-purchases-layout";

/** /procurement/feed-purchases: header + Record, the farm / delivery toolbar, the purchases table card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <ToolbarSkeleton fields={[200, 200]} />
      {/* Contract table "feed-purchases": 11 columns. */}
      <TableSkeleton columns={11} rows={DEFAULT_LIMIT} />
    </PageSkeleton>
  );
}
