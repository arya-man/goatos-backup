// The four-lane board width the /tasks and /work-board kanbans use, shared with their skeletons
// (KanbanSkeleton laneWidth) so the loading lanes are the loaded lanes. Full width on a phone, where
// the lanes STACK (KanbanBoard; the 86vw swipe-one-lane layout hid three lanes with no cue and let a
// hidden lane's height blank the page, PR #294 A2/E6); four lanes filling the column from sm up,
// capped at the template column width. The gap term is the board's 24px column gap (--sp-3).
export const FOUR_LANE_COLUMN_WIDTH = {
  xs: "100%",
  sm: "clamp(calc(30 * var(--spacing)), calc((100% - 3 * calc(3 * var(--spacing))) / 4), var(--kanban-col-w))",
} as const;
