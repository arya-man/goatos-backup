// The four-lane board width the /tasks and /work-board kanbans use, shared with their skeletons
// (KanbanSkeleton laneWidth) so the loading lanes are the loaded lanes. 86vw on a phone (one lane
// and the edge of the next, snap-scrolled); four lanes filling the column from sm up, capped at the
// template column width. The gap term is the board's 24px column gap (--sp-3).
export const FOUR_LANE_COLUMN_WIDTH = {
  xs: "86vw",
  sm: "clamp(calc(var(--sp-5) * 6), calc((100% - 3 * var(--sp-3)) / 4), var(--kanban-col-w))",
} as const;
