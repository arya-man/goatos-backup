import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { CATALOG_PAGE_SIZE, HEALTH_CONFIG_HEADER_LAYOUT } from "@/features/health/health-config-layout";

/** /health/config: header + Add disease, the three rulebook tabs, the protocol catalogue card (search, two filters, keyset pager). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton layout={HEALTH_CONFIG_HEADER_LAYOUT} crumbLink={false} actions={1} />
      <TabsSkeleton count={3} />
      {/* Contract table "protocol-catalog" + the trailing actions column. */}
      <TableSkeleton columns={9} rows={CATALOG_PAGE_SIZE} toolbar={<FilterCardSkeleton inCard fields={["search", 180, 180]} />} />
    </PageSkeleton>
  );
}
