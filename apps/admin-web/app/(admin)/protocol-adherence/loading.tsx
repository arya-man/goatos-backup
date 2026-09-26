import { FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { WORK_STATE_ORDER } from "@/features/process-integrity/process-integrity";

/**
 * /protocol-adherence (template order list): header, four CourseWidgetSummary cards, then ONE ledger
 * card: CardHeader, work-state tabs with counts, toolbar (severity select, Filters), 7-column table, pager.
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={4} icon hint />
      <TableSkeleton
        columns={7}
        rows={10}
        headerAction
        tabs={<TabsSkeleton count={WORK_STATE_ORDER.length + 1} counts />}
        toolbar={<FilterCardSkeleton inCard fields={[200, 120]} />}
      />
    </PageSkeleton>
  );
}
