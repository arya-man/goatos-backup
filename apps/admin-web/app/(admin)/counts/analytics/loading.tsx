import { ChartCardSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/**
 * /counts/analytics: header + export, date range, six EcommerceWidgetSummary cards, the flow chart
 * (lg 8) beside the sex radial (lg 4), then the three composition cards.
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      <PageHeaderSkeleton actions={1} />
      <ToolbarSkeleton fields={[300]} small={false} />
      <KpiRowSkeleton count={6} shapes={[{ hint: false }, { hint: true }, { spark: true }, { spark: true }, { spark: true }, {}]} />
      <GridSkeleton
        items={[
          { size: { xs: 12, lg: 8 }, node: <ChartCardSkeleton height={364} /> },
          { size: { xs: 12, lg: 4 }, node: <ChartCardSkeleton height={364} /> },
        ]}
      />
      <GridSkeleton
        items={[0, 1, 2].map(() => ({ size: { xs: 12, md: 6, lg: 4 }, node: <ChartCardSkeleton height={240} /> }))}
      />
    </PageSkeleton>
  );
}
