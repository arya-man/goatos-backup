import { DetailCardSkeleton, GridSkeleton, ListCardSkeleton, OptionalSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, StatGridCardSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { SHED_DRIVE_ROW_COLUMNS, SHED_WORK_STATE_COLUMNS, SHED_WORK_STATE_KEYS } from "@/features/vaccination-execution/shed-detail-layout";

/**
 * /vaccination/execution/sheds/[shedId]: back-link header (title + section • park crumbs, no action),
 * then the 8/4 grid: the Work state stat grid, the Blocked card (optional) and the Drive rows table beside the pen summary card.
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton back titleWidth={220} crumbWidths={[290, 84]} />
      <GridSkeleton
        items={[
          {
            size: { xs: 12, md: 8 },
            node: (
              <StackSkeleton>
                <StatGridCardSkeleton cells={SHED_WORK_STATE_KEYS.length} columns={SHED_WORK_STATE_COLUMNS} />
                {/* Blocked / deferred: only when a drive row carries a blocker. */}
                <OptionalSkeleton>
                  <ListCardSkeleton rows={1} avatar={false} />
                </OptionalSkeleton>
                <TableSkeleton columns={SHED_DRIVE_ROW_COLUMNS} rows={3} pager={false} />
              </StackSkeleton>
            ),
          },
          { size: { xs: 12, md: 4 }, node: <DetailCardSkeleton rows={9} header={false} /> },
        ]}
      />
    </PageSkeleton>
  );
}
