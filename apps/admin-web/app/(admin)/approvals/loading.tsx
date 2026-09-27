import { FilterCardSkeleton, KpiRowSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { APPROVALS_COPY, TYPE_TABS } from "@/features/approvals/copy";

/**
 * /approvals, block for block with ApprovalsPage: header (no crumb: it only repeats the title), then
 * one Stack spacing 3: the four CourseWidgetSummary tiles (rendered only when the view has rows) on
 * the page's `sm 6 / md 3` grid, the queue card (type tabs, status / farm / raised toolbar, rows).
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbs={false} />
      <StackSkeleton>
        <OptionalSkeleton>
          <KpiRowSkeleton count={4} size={{ xs: 12, sm: 6, md: 3 }} />
        </OptionalSkeleton>
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
