import { ChartCardSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /counts/analytics: header + export, date range, six KPI cards, the flow chart, then the composition cards. */
export default function Loading() {
  return (
    <PageSkeleton root="kit-enter pagegrid ha-kit-stack">
      <PageHeaderSkeleton actions={1} />
      <ToolbarSkeleton fields={[280]} />
      <KpiRowSkeleton count={6} shapes={[{ spark: true }, { spark: true }, { spark: true }, {}, { parts: true }, {}]} />
      <ChartCardSkeleton height={300} />
      <StackSkeleton spacing={2.75}>
        <ChartCardSkeleton height={220} />
        <ChartCardSkeleton height={160} />
        <ChartCardSkeleton height={120} />
        <ChartCardSkeleton height={80} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
