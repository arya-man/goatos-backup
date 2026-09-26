import { ChartCardSkeleton, FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /counts/breakdown: header, the count deck, the pen table card (filters in the card, pager), the three distribution charts. */
export default function Loading() {
  return (
    <PageSkeleton className="counts-breakdown-page">
      <PageHeaderSkeleton />
      <KpiRowSkeleton count={6} shapes={[{}, { parts: true }, {}, {}, {}, {}]} />
      <TableSkeleton columns={8} rows={10} header={false} toolbar={<FilterCardSkeleton inCard fields={[160, 160, 160, 160, 160]} />} />
      <StackSkeleton>
        <ChartCardSkeleton height={260} />
        <ChartCardSkeleton height={260} />
        <ChartCardSkeleton height={260} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
