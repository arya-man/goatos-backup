import { FilterCardSkeleton, KanbanSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton } from "@/components/app/skeletons";
import { FOUR_LANE_COLUMN_WIDTH } from "@/components/app/kanban/board-layout";
import { WB_DAY_STEPPER_WIDTH, WB_SKELETON_LANES, WB_TOOLBAR_FIELDS } from "@/features/work-board/work-board-layout";

/**
 * /work-board, block for block with WorkBoardPage: header (template CustomBreadcrumbs mb 5 = the
 * stack gap + 2), the bare toolbar row (assignee, module, search, day stepper), then the template
 * kanban on the four-lane width (86vw lanes on a phone).
 */
export default function Loading() {
  return (
    <PageSkeleton root="wb">
      <StackSkeleton>
        <PageHeaderSkeleton crumbLink={false} titleWidth={90} mb={{ xs: 0, md: 2 }} />
        <FilterCardSkeleton bare searchSmall fields={WB_TOOLBAR_FIELDS} actionWidths={[WB_DAY_STEPPER_WIDTH]} />
        <KanbanSkeleton lanes={WB_SKELETON_LANES} laneWidth={FOUR_LANE_COLUMN_WIDTH} />
      </StackSkeleton>
    </PageSkeleton>
  );
}
