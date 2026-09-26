import { DetailCardSkeleton, GridSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";

/** /vaccination/execution/sheds/[shedId]: back header, then the 8/4 grid: work-state and drive-row cards beside the pen summary card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton actionWidths={[88]} />
      <GridSkeleton
        items={[
          {
            size: { xs: 12, md: 8 },
            node: (
              <StackSkeleton>
                <DetailCardSkeleton rows={3} columns={3} />
                {/* Contract table "shed-drive-rows": 6 columns. */}
                <TableSkeleton columns={6} rows={6} pager={false} />
              </StackSkeleton>
            ),
          },
          { size: { xs: 12, md: 4 }, node: <DetailCardSkeleton rows={9} header={false} /> },
        ]}
      />
    </PageSkeleton>
  );
}
