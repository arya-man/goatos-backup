import { BlockSkeleton, DetailCardSkeleton, OrderDetailsToolbarSkeleton, GridSkeleton, ListCardSkeleton, OptionalSkeleton, PageSkeleton } from "@/components/app/skeletons";
import { WF_BLOCKER_HEIGHT, WF_DETAIL_GRID, WF_ROWS, WF_TOOLBAR } from "@/features/process-integrity/workflow-drilldown-layout";

/**
 * /workflows/[row_id], block for block with the drilldown: the template OrderDetailsToolbar (back,
 * title + work-state Label, subtitle, the state Labels + Action Center), the blocker Alert (only for a
 * blocked row), then the chain card beside the drive / summary card.
 */
export default function Loading() {
  return (
    <PageSkeleton>
      <OrderDetailsToolbarSkeleton flush titleWidth={WF_TOOLBAR.titleWidth} titleLines={WF_TOOLBAR.titleLines} subtitleLines={WF_TOOLBAR.subtitleLines} actions={WF_TOOLBAR.actions} wrapActions />
      <OptionalSkeleton>
        <BlockSkeleton height={WF_BLOCKER_HEIGHT} card={false} />
      </OptionalSkeleton>
      <GridSkeleton
        items={[
          { size: WF_DETAIL_GRID.chain, node: <ListCardSkeleton rows={WF_ROWS.chain} /> },
          { size: WF_DETAIL_GRID.side, node: <DetailCardSkeleton rows={WF_ROWS.side} header={false} /> },
        ]}
      />
    </PageSkeleton>
  );
}
