import { FilterCardSkeleton, FormCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { DEFAULT_PAGE_SIZE, LEAVE_LIST_HEADER_SX, LEAVE_QUEUE_HEADER_SX, LEAVE_TILE_SIZE, LEAVE_TOOLBAR_FIELDS, STATUS_FILTERS } from "@/features/leave/leave-layout";

/**
 * /leave, block for block with LeavePage: header (crumbs), then the page Stack: the four status tiles, the "who approves" settings card, the "Waiting for you" queue card
 * (header, the toolbar: park + designation selects, the date pair, search and the ⋮, then rows), and
 * the "All leave requests" card with its status tabs and the same toolbar.
 */
export default function Loading() {
  const toolbar = <FilterCardSkeleton inCard fields={LEAVE_TOOLBAR_FIELDS} actionWidths={[36]} />;
  return (
    <PageSkeleton className="leave-page">
      <PageHeaderSkeleton titleWidth={80} crumbLink={false} />
      <StackSkeleton>
        <KpiRowSkeleton count={4} size={LEAVE_TILE_SIZE} />
        <FormCardSkeleton wrap controls={[232, 56]} action={64} />
        <TableSkeleton columns={5} rows={DEFAULT_PAGE_SIZE} headerSx={LEAVE_QUEUE_HEADER_SX} toolbar={toolbar} pager={false} />
        <TableSkeleton columns={6} rows={DEFAULT_PAGE_SIZE} headerSx={LEAVE_LIST_HEADER_SX} tabs={<TabsSkeleton count={STATUS_FILTERS.length} counts />} toolbar={toolbar} pager={false} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
