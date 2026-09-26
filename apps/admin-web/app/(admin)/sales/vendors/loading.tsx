import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { PAGE_SIZE, VENDOR_FILTER_KEYS } from "@/features/procurement/vendor-layout";

/** sales vendors: header + Add, the search + five-select filter card, the vendor register card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <FilterCardSkeleton fields={["search", ...VENDOR_FILTER_KEYS.map(() => 160)]} actions={1} />
      <TableSkeleton columns={5} rows={PAGE_SIZE} />
    </PageSkeleton>
  );
}
