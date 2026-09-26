import { ChartCardSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /sales/loads: header with the view pills, farm toggle, the load-wise card (KPI deck, progress rows, charts, table). */
export default function Loading() {
  return (
    <PageSkeleton className="sales-loads-page">
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={2} variant="pill" />} />
      <ChartCardSkeleton height={1200} />
    </PageSkeleton>
  );
}
