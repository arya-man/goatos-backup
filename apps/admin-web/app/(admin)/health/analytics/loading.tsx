import { ChartCardSkeleton, ControlRowSkeleton, FieldSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/**
 * /health/analytics, block for block with HealthAnalyticsPage (one `Stack spacing={3}`): header +
 * Health config, the window row (WindowDateFilter field, the five-preset SegmentedLinks strip, the
 * read-only scope caption wrapping under them), five KpiWidget course cards on the page's own
 * `{ xs: 12, sm: 6, md: 4 }` grid (3 + 2), the six-tab strip with counts, the overview chart cards.
 */
export default function Loading() {
  return (
    <PageSkeleton root="" gap={3}>
      <PageHeaderSkeleton actionWidths={[150]} />
      <ControlRowSkeleton caption={200}>
        <FieldSkeleton width={296} height={56} />
        <TabsSkeleton count={5} variant="pill" />
      </ControlRowSkeleton>
      <KpiRowSkeleton count={5} hint size={{ xs: 12, sm: 6, md: 4 }} />
      <TabsSkeleton count={6} counts />
      <ChartCardSkeleton height={300} subheader legend />
      <ChartCardSkeleton height={160} subheader />
    </PageSkeleton>
  );
}
