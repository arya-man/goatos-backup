import { PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";
import { APPROVALS_COPY, TYPE_TABS } from "@/features/approvals/copy";

/** /approvals: header, the type tabs + status / farm / date toolbar, the queue table card (first / next pager). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbs={false} />
      <ToolbarSkeleton left={<TabsSkeleton count={TYPE_TABS.length} />} fields={[140, 160, 200]} />
      <TableSkeleton columns={APPROVALS_COPY.table.columns.length} rows={20} />
    </PageSkeleton>
  );
}
