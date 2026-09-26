import { HeadingSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, StatStripSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /sales/farm-value: header, farm toggle, the valuation block (heading + three KPI cards), the by-category breakdown card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <ToolbarSkeleton left={<TabsSkeleton count={2} variant="pill" />} />
      <StackSkeleton spacing={2}>
        <HeadingSkeleton />
        <KpiRowSkeleton count={3} hero icon hint />
      </StackSkeleton>
      <StatStripSkeleton count={5} meta />
    </PageSkeleton>
  );
}
