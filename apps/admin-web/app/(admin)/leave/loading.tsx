import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, StatStripSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { DEFAULT_PAGE_SIZE, STATUS_FILTERS } from "@/features/leave/leave-layout";

/** /leave: header, then the page stack: counts strip, approvals queue card, requests card with status tabs. */
export default function Loading() {
  return (
    <PageSkeleton className="leave-page">
      <PageHeaderSkeleton />
      <StackSkeleton>
        <StatStripSkeleton count={4} />
        <TableSkeleton columns={6} rows={DEFAULT_PAGE_SIZE} headerAction toolbar={<FilterCardSkeleton inCard fields={["search", 160, 160, 240]} />} />
        <TableSkeleton columns={6} rows={DEFAULT_PAGE_SIZE} tabs={<TabsSkeleton count={STATUS_FILTERS.length} counts />} toolbar={<FilterCardSkeleton inCard fields={["search", 160, 160, 240]} />} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
