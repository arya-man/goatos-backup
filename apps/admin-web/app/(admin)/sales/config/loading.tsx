import { PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { DEFAULT_LIMIT } from "@/features/procurement/sales-config-layout";

/** /sales/config: header + Record sale / Tag animals, the sales entry card, the load entry card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={2} />
      {/* Contract table "sales-deals": 9 columns. */}
      <TableSkeleton columns={9} rows={DEFAULT_LIMIT} />
      <TableSkeleton columns={6} rows={6} pager={false} />
    </PageSkeleton>
  );
}
