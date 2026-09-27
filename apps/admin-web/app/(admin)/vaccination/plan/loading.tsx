import { ChipSkeleton, KpiRowSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { EARLIER_HEAD_CELLS, LIVE_HEAD_CELLS, LIVE_PAGE_SIZE, PLAN_FACT_KEYS, PLAN_FACT_SIZE } from "@/features/vaccination-plan/plan-layout";

/**
 * /vaccination/plan, block for block with PlanConsole (layout from plan-layout.ts): header + "Start a
 * new version", then its Stack spacing 3 — the live version's fact tiles (CardHeader + icon avatar),
 * the live vaccine table card (title, "Live right now", status Label), earlier versions (only with rows).
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} actionWidths={[176]} />
      <StackSkeleton>
        <KpiRowSkeleton count={PLAN_FACT_KEYS.length} size={PLAN_FACT_SIZE} fact />
        <TableSkeleton columns={LIVE_HEAD_CELLS.length} rows={LIVE_PAGE_SIZE} subheader headerAction={<ChipSkeleton width={91} height={24} />} />
        <OptionalSkeleton>
          <TableSkeleton columns={EARLIER_HEAD_CELLS.length} rows={3} headerAction={<ChipSkeleton width={40} height={24} />} pager={false} />
        </OptionalSkeleton>
      </StackSkeleton>
    </PageSkeleton>
  );
}
