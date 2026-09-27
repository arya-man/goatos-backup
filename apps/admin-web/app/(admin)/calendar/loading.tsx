import { BlockSkeleton, ChipRowSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton } from "@/components/app/skeletons";

/** /calendar: header with the Week / History switch, owner chips, workstream tabs, the full calendar card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton crumbLink={false} actionWidths={[180]} />
      <ChipRowSkeleton count={4} />
      <TabsSkeleton count={4} />
      <BlockSkeleton height={{ xs: "70vh", md: "calc(100dvh - 220px)" }} />
    </PageSkeleton>
  );
}
