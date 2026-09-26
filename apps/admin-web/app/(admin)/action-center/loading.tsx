import { ChipRowSkeleton, KanbanSkeleton, PageHeaderSkeleton, PageSkeleton, PagerSkeleton, StackSkeleton, TabsSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** /action-center: header, board / verify tabs, then the board panel (chip + search toolbar, severity chips, lanes, pager). */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <TabsSkeleton count={2} counts />
      <StackSkeleton spacing={2}>
        <ToolbarSkeleton left={<ChipRowSkeleton count={4} />} fields={["search", 120, 120]} />
        <ChipRowSkeleton count={4} />
        <KanbanSkeleton layout="grid" lanes={[3, 3, 2, 2, 1]} minHeight={360} />
        <PagerSkeleton />
      </StackSkeleton>
    </PageSkeleton>
  );
}
