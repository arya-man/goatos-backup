import { DetailCardSkeleton, GridSkeleton, ListCardSkeleton, PageHeaderSkeleton, PageSkeleton } from "@/components/app/skeletons";

/** /workflows/[row_id]: back header with the status chips, then the chain card beside the summary card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actionWidths={[72, 72, 72, 72, 72]} />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 8 }, node: <ListCardSkeleton rows={7} /> },
          { size: { xs: 12, md: 4 }, node: <DetailCardSkeleton rows={6} header={false} /> },
        ]}
      />
    </PageSkeleton>
  );
}
