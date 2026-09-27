import { ChipSkeleton, KpiRowSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";

/**
 * /vaccination/plan, block for block with PlanConsole: header + "Start a new version", then its
 * Stack spacing 3 — the live version's four fact tiles (CardHeader + icon avatar, sm 6 / md 3), the
 * live vaccine table card (title, "Live right now", status Label), earlier versions (only with rows).
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} actionWidths={[176]} />
      <StackSkeleton>
        <KpiRowSkeleton count={4} size={{ xs: 12, sm: 6, md: 3 }} fact />
        <TableSkeleton columns={4} rows={7} subheader headerAction={<ChipSkeleton width={91} height={24} />} />
        <OptionalSkeleton>
          <TableSkeleton columns={5} rows={3} headerAction={<ChipSkeleton width={40} height={24} />} pager={false} />
        </OptionalSkeleton>
      </StackSkeleton>
    </PageSkeleton>
  );
}
