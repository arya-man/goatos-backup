import { FilterCardSkeleton, KpiRowSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /routines: header + Create routine, then the page stack: today's tiles, the routines card, the tasks card. */
export default function Loading() {
  return (
    <PageSkeleton className="proc-mx routines-page">
      <PageHeaderSkeleton crumbs={false} actions={1} />
      <StackSkeleton>
        <OptionalSkeleton>
          <KpiRowSkeleton count={5} />
        </OptionalSkeleton>
        <TableSkeleton columns={7} rows={6} headerAction toolbar={<FilterCardSkeleton inCard fields={["search", 160, 160, 160]} />} />
        <TableSkeleton columns={5} rows={6} headerAction toolbar={<FilterCardSkeleton inCard fields={["search", 160, 160, 160, 240]} />} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
