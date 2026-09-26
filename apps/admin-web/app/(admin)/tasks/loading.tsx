import { FilterCardSkeleton, KanbanSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";
import { TASK_BOARD_COLUMNS } from "@/features/leadership-tasks/task-url";

/** /tasks: header + New task, the sticky scope / filter group, the task board. */
export default function Loading() {
  return (
    <PageSkeleton className="lt-page">
      <PageHeaderSkeleton actions={1} />
      <StackSkeleton spacing={2}>
        <ToolbarSkeleton left={<TabsSkeleton count={3} variant="pill" />} fields={[160]} />
        <FilterCardSkeleton inCard fields={["search", 160, 160, 160, 200, 200]} small />
      </StackSkeleton>
      <KanbanSkeleton layout="grid" lanes={TASK_BOARD_COLUMNS.map((_, i) => 3 - (i % 2))} minHeight={560} />
    </PageSkeleton>
  );
}
