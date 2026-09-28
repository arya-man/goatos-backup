import { ChartCardSkeleton, ControlRowSkeleton, FieldSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { HealthKpiSkeleton } from "@/features/health/health-analytics-layout";

/**
 * /health/analytics, block for block with HealthAnalyticsPage (one `Stack spacing={3}`): header +
 * Health config, the window row (WindowDateFilter field, the five-preset SegmentedLinks strip; no
 * scope caption), five KpiWidget course cards (3 at md 4, then 2 at md 6: no orphan), the six-tab strip with counts, the overview chart cards.
 */
export default function Loading() {
  return (
    <PageSkeleton root="" gap={3}>
      <PageHeaderSkeleton crumbLink={false} titleWidth={180} actionWidths={[132]} />
      <ControlRowSkeleton>
        <FieldSkeleton width={{ xs: "100%", md: 296 }} height={{ xs: 44, md: 56 }} />
        <TabsSkeleton count={5} variant="pill" />
      </ControlRowSkeleton>
      <HealthKpiSkeleton />
      <TabsSkeleton count={6} counts />
      <ChartCardSkeleton height={300} subheader legend />
      <ChartCardSkeleton height={160} subheader />
    </PageSkeleton>
  );
}
