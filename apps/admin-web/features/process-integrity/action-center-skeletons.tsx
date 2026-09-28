// Action Center loading shapes, shared by the route loading.tsx and the page's own UrlSuspense
// fallbacks so a hard load and a tab / filter click paint ONE shape (TR1-#1, REVIEW-35):
// quick-tile KPI row, toolbar card, template kanban lanes, pager.
// guard: action-center-loading-mirrors-page
import { ChipRowSkeleton, KanbanSkeleton, KpiRowSkeleton, PagerSkeleton, StackSkeleton, TableSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

/** The board toolbar card's controls, as the page renders them (labels measured at 390 / 1440). */
export const AC_TOOLBAR = { chipWidths: [93, 67, 63, 62, 44], buttonWidths: [44, 100, 84] } as const;

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
      {/* The board toolbar CARD (TR3-P0-5): the five severity chips (44px tap pills below sm), the
          visible-cards search (44 on a phone, the 56px field from md), the paging ⓘ, My tasks, Filters. */}
      <ToolbarSkeleton card left={<ChipRowSkeleton count={AC_TOOLBAR.chipWidths.length} widths={[...AC_TOOLBAR.chipWidths]} height={{ xs: 44, sm: 32 }} />} fields={["search", ...AC_TOOLBAR.buttonWidths]} searchHeight={{ xs: 44, md: 56 }} />
      {BOARD_LANES_SKELETON}
    </StackSkeleton>
  ),
  verify: <TableSkeleton columns={4} rows={10} toolbar={<ToolbarSkeleton fields={["search", 120]} sx={{ p: 2.5 }} />} />,
};
