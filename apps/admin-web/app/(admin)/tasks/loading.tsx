import { ControlsCardSkeleton, FilterCardSkeleton, KanbanSkeleton, PageHeaderSkeleton, PageSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { TASK_BOARD_COLUMNS } from "@/features/leadership-tasks/task-url";

/** /tasks: header (view switch + New task), ONE filter card (status tabs with counts, toolbar with the scope select first), the task board. */
export default function Loading() {
  return (
    <PageSkeleton className="lt-page">
      <PageHeaderSkeleton actions={2} />
      <ControlsCardSkeleton tabs={<TabsSkeleton count={5} counts />} toolbar={<FilterCardSkeleton inCard fields={[200, 160, 160, 160, "search", 200]} small />} />
      <KanbanSkeleton layout="grid" lanes={TASK_BOARD_COLUMNS.map((_, i) => 3 - (i % 2))} minHeight={560} />
    </PageSkeleton>
  );
}
