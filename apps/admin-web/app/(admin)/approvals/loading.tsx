import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { APPROVALS_COPY, TYPE_TABS } from "@/features/approvals/copy";

/**
 * /approvals, block for block with ApprovalsPage: header (no crumb: it only repeats the title), then
 * one Stack spacing 3 holding the queue card (type tabs, status / farm / raised toolbar, rows).
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbs={false} />
      <StackSkeleton>
        <TableSkeleton
          columns={APPROVALS_COPY.table.columns.length}
          rows={10}
          header={false}
          tabs={<TabsSkeleton count={TYPE_TABS.length} />}
          toolbar={<FilterCardSkeleton inCard fields={[160, 200, 296]} />}
        />
      </StackSkeleton>
    </PageSkeleton>
  );
}
