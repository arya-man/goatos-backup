import { ChipRowSkeleton, FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { SEVERITY_ORDER, WORK_STATE_ORDER } from "@/features/process-integrity/process-integrity";

/** /protocol-adherence: header, four KPI cards, work-state tabs, severity chips, the ledger table card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={4} icon hint />
      <TabsSkeleton count={WORK_STATE_ORDER.length + 1} counts />
      <ChipRowSkeleton count={SEVERITY_ORDER.length + 1} />
      {/* Contract table "adherence-ledger": 7 columns. */}
      <TableSkeleton columns={7} rows={10} toolbar={<FilterCardSkeleton inCard fields={[120]} small />} />
    </PageSkeleton>
  );
}
