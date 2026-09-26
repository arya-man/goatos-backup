import { ChartCardSkeleton, FilterCardSkeleton, GridSkeleton, HeadingSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";

/** /weighing/weights: header + export, the filter card, the meta line, the KPI decks, the breed / sex / stage charts. */
export default function Loading() {
  return (
    <PageSkeleton root="weights-page">
      <PageHeaderSkeleton actions={1} />
      <FilterCardSkeleton fields={[200, 260, 200, 160]} />
      <HeadingSkeleton variant="body2" width={320} />
      <KpiRowSkeleton count={5} shapes={[{}, { parts: true }, {}, {}, {}]} />
      <KpiRowSkeleton count={1} spark trend />
      <GridSkeleton items={[0, 1, 2].map(() => ({ size: { xs: 12, md: 4 }, node: <ChartCardSkeleton height={260} /> }))} />
    </PageSkeleton>
  );
}
