import { ControlsCardSkeleton, FilterCardSkeleton, KanbanSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { FOUR_LANE_COLUMN_WIDTH } from "@/components/app/kanban/board-layout";
import { TASK_BOARD_SKELETON_LANES, TASK_HEADER_ACTION_HEIGHTS, TASK_HEADER_ACTION_WIDTHS, TASK_DATES_BUTTON_WIDTH, TASK_STATUS_TAB_COUNT, TASK_TOOLBAR_FIELDS } from "@/features/leadership-tasks/tasks-layout";

/**
 * /tasks: header (view toggle + New task; plain crumbs), ONE filter card (status tabs with counts,
 * the toolbar: scope, assignee, raised by, sort, then search + Dates), then the template kanban board
 * on the page's four-lane width (86vw lanes on a phone).
 */
export default function Loading() {
  return (
    <PageSkeleton className="lt-page">
      <PageHeaderSkeleton crumbLink={false} titleWidth={80} actionWidths={TASK_HEADER_ACTION_WIDTHS} actionHeights={TASK_HEADER_ACTION_HEIGHTS} />
      <ControlsCardSkeleton tabs={<TabsSkeleton count={TASK_STATUS_TAB_COUNT} counts />} toolbar={<FilterCardSkeleton inCard fields={TASK_TOOLBAR_FIELDS} actionWidths={[TASK_DATES_BUTTON_WIDTH]} />} />
      <KanbanSkeleton lanes={TASK_BOARD_SKELETON_LANES} laneWidth={FOUR_LANE_COLUMN_WIDTH} />
    </PageSkeleton>
  );
}
