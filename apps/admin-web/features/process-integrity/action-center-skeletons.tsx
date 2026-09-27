// Action Center loading shapes, shared by the route loading.tsx and the page's own UrlSuspense
// fallbacks so a hard load and a tab / filter click paint ONE shape (TR1-#1, REVIEW-35):
// quick-tile KPI row, toolbar card, template kanban lanes, pager.
// guard: action-center-loading-mirrors-page
import { ChipRowSkeleton, KanbanSkeleton, KpiRowSkeleton, PagerSkeleton, StackSkeleton, TableSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

export const BOARD_LANES_SKELETON = (
  <StackSkeleton spacing={2}>
    <KanbanSkeleton lanes={[3, 3, 2, 2, 1]} />
    <PagerSkeleton />
  </StackSkeleton>
);

/** Each view's skeleton ("" = the status board, the default). */
export const VIEW_SKELETON = {
  "": (
    <StackSkeleton spacing={3}>
      <KpiRowSkeleton count={3} icon />
      <ToolbarSkeleton left={<ChipRowSkeleton count={4} />} fields={["search", 120, 120]} />
      {BOARD_LANES_SKELETON}
    </StackSkeleton>
  ),
  verify: <TableSkeleton columns={4} rows={10} toolbar={<ToolbarSkeleton fields={["search", 120]} sx={{ p: 2.5 }} />} />,
};
