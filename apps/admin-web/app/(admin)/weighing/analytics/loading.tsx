import { FilterCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { WEIGHTS_DEFAULT_LIMIT, WEIGHTS_TABS } from "@/features/weighing/weights-analytics-layout";

/** /weighing/analytics: header + export with the eight analytics tabs, the filter card, the General tab (KPI decks, pens table). */
export default function Loading() {
  return (
    <PageSkeleton root="weights-page">
      <PageHeaderSkeleton actions={1} tabs={<TabsSkeleton count={WEIGHTS_TABS.length} />} />
      <FilterCardSkeleton fields={[200, 260, 200, 160, 160]} />
      <StackSkeleton>
        <KpiRowSkeleton count={5} shapes={[{}, { parts: true }, {}, {}, {}]} />
        <KpiRowSkeleton count={3} shapes={[{ spark: true, trend: true }, {}, {}]} />
        <TableSkeleton columns={9} rows={WEIGHTS_DEFAULT_LIMIT} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
