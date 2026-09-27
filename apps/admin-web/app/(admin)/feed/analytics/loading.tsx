import { ChartCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /feed/analytics: header + export, the tab bar with range chips, the overview KPI deck and charts. */
export default function Loading() {
  return (
    <PageSkeleton root="kit-enter pagegrid feed-analytics-page">
      <PageHeaderSkeleton crumbLink={false} actions={1} />
      <ToolbarSkeleton left={<TabsSkeleton count={6} />} fields={["chip", "chip", "chip"]} />
      <KpiRowSkeleton count={5} shapes={[{ spark: true, trend: true }, { spark: true, trend: true }, { spark: true, trend: true }, { hint: true }, { hint: true }]} />
      <ChartCardSkeleton height={320} legend />
    </PageSkeleton>
  );
}
