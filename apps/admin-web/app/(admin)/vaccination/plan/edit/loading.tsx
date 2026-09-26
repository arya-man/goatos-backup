import { BlockSkeleton, DetailCardSkeleton, GridSkeleton, ListCardSkeleton, PageHeaderSkeleton, PageSkeleton, StatStripSkeleton } from "@/components/app/skeletons";

/** /vaccination/plan/edit: header + draft pill / back, the scope tiles, the vaccine rail beside the vaccine editor, the action bar. */
export default function Loading() {
  return (
    <PageSkeleton root="vplan">
      <PageHeaderSkeleton actionWidths={[72, 128]} />
      <StatStripSkeleton count={3} />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 3 }, node: <ListCardSkeleton rows={6} avatar={false} /> },
          { size: { xs: 12, md: 9 }, node: <DetailCardSkeleton rows={8} columns={2} /> },
        ]}
      />
      <BlockSkeleton height={72} />
    </PageSkeleton>
  );
}
