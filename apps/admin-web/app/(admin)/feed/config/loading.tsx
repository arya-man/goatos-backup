import { FilterCardSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";

/**
 * /feed/config: header, then four template table cards (Card + CardHeader): the ration grid (five
 * filters, pager), the experiment pens (header action, filters, pager), the session template and
 * the feed day clock (no pager).
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      {/* Contract table "ration-grid": 6 columns + the edit column. */}
      <TableSkeleton columns={7} rows={10} toolbar={<FilterCardSkeleton inCard fields={[180, 180, 180, 180, 180]} />} />
      {/* Contract table "experiment-config": 7 columns + the edit column. */}
      <TableSkeleton columns={8} rows={3} subheader headerAction toolbar={<FilterCardSkeleton inCard fields={[180, 180, 180, 180, 180]} />} />
      {/* Session template: one row per feeding session. */}
      <TableSkeleton columns={5} rows={2} subheader pager={false} />
      {/* Feed day clock: one row per workflow. */}
      <TableSkeleton columns={6} rows={2} subheader pager={false} />
    </PageSkeleton>
  );
}
