import { ChartCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /health/analytics: header + config link, window filter, five KPI cards, the view tabs, the overview charts. */
export default function Loading() {
  return (
    <PageSkeleton root="pagegrid ha-kit-stack">
      <PageHeaderSkeleton actions={1} />
      <ToolbarSkeleton left={<TabsSkeleton count={5} variant="pill" />} fields={[280]} />
      <KpiRowSkeleton count={5} icon hint shapes={[{ spark: true, hint: true }, { spark: true, hint: true }, { icon: true, hint: true }, { icon: true, hint: true }, { icon: true, hint: true }]} />
      <TabsSkeleton count={6} counts />
      <ChartCardSkeleton height={300} />
      <ChartCardSkeleton height={160} />
    </PageSkeleton>
  );
}
