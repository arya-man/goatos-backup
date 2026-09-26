import { ChartCardSkeleton, HeadingSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /sales/sold: header, farm toggle, the Sold heading, the KPI deck, the weight-band strip, the monthly charts. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={2} variant="pill" />} />
      <HeadingSkeleton />
      <KpiRowSkeleton count={4} hero spark hint />
      <StatStripSkeleton count={4} meta />
      <ChartCardSkeleton height={{ xs: 780, md: 960 }} />
    </PageSkeleton>
  );
}
