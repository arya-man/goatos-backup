import { DetailCardSkeleton, GridSkeleton, ListCardSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton } from "@/components/app/skeletons";

/** /vaccination/plan/edit: header + draft Label / back, the scope strip card, the vaccine selector column (md 4) beside the vaccine form card (md 8). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actionWidths={[120, 132]} />
      <StatStripSkeleton count={3} />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 4 }, node: <ListCardSkeleton rows={7} /> },
          { size: { xs: 12, md: 8 }, node: <DetailCardSkeleton rows={8} /> },
        ]}
      />
    </PageSkeleton>
  );
}
