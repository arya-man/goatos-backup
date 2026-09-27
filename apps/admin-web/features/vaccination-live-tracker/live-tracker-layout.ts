// Layout constants shared by the Live Drive Tracker board (live-tracker-board.tsx, -filters, -kpis)
// and its loading twin (live-tracker-skeleton.tsx), so the skeleton cannot drift from the page.
/** Space under the page header (the board's blocks are stacked with margins, not a grid gap). */
export const LT_HEADER_MB = { xs: 3, md: 5 } as const;
/** Space under the AppWelcome row and under the filter card. */
export const LT_BLOCK_MB = 3;
/** The filter selects, in order (buildFilters). */
export const LT_FILTER_IDS = ["lt_park", "lt_vaccine", "lt_operator", "lt_shed", "lt_status"] as const;
/** The filter card's grid track floor from sm (`repeat(auto-fill, minmax(200px, 1fr))`). */
export const LT_FILTER_MIN = 200;
/** The KPI deck's KpiGrid card floor. */
export const LT_KPI_MIN = 200;
/** Tables column (operators / pens / combo) beside the live rail. */
export const LT_MAIN_SIZE = { xs: 12, lg: 8 } as const;
export const LT_RAIL_SIZE = { xs: 12, lg: 4 } as const;
/** The KPI deck's tiles, in order (rendered only once the day has counts). */
export const LT_KPI_KEYS = ["scheduled", "proofs", "scans", "remaining", "combo", "attention"] as const;
