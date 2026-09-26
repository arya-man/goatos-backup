import { KpiRowSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /vaccination/plan: header + version action, the live version's four CourseWidgetSummary tiles, the live vaccine table card, earlier versions. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={4} icon />
      <TableSkeleton columns={4} rows={7} subheader headerAction />
      <OptionalSkeleton>
        <TableSkeleton columns={5} rows={3} headerAction pager={false} />
      </OptionalSkeleton>
    </PageSkeleton>
  );
}
