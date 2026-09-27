// Layout constants shared by the /workflows/[row_id] drilldown (workflow-drilldown.tsx) and its
// loading.tsx, so the skeleton cannot drift from the page.
/** The chain card beside the drive / summary card. */
export const WF_DETAIL_GRID = { chain: { xs: 12, md: 8 }, side: { xs: 12, md: 4 } } as const;

// Loading-twin estimates of what the page does not set itself (copy-driven), measured at 1440 and 390.
/** OrderDetailsToolbar: title width, subtitle lines, the four state Labels + the Action Center button. */
export const WF_TOOLBAR = {
  titleWidth: 364,
  titleLines: { xs: 2, md: 1 },
  subtitleLines: 2,
  actions: [{ width: 49, label: true }, { width: 107, label: true }, { width: 94, label: true }, { width: 107, label: true }, { width: 140 }],
} as const;
/** The blocker Alert (only when the row is blocked). */
export const WF_BLOCKER_HEIGHT = { xs: 72, md: 50 } as const;
/** Chain card rows; summary card rows. */
export const WF_ROWS = { chain: 10, side: 6 } as const;
