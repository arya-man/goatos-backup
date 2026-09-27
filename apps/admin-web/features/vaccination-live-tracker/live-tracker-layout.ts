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

// Loading-twin estimates of rendered sizes the board does not set itself (copy- and data-driven),
// measured on the served page at 1440 and 390.
/** Header: the two crumbs' widths ("Preventive Care (PC) · operations" wraps to a second line on a phone) and the two buttons. */
export const LT_HEADER = { crumbWidths: [230, 130] as [number, number], actionWidths: [150, 151] } as const;
/** The AppWelcome drive-day row's height (title, parks Label, empty-day text, poller). */
export const LT_WELCOME_HEIGHT = { xs: 374, md: 324 } as const;
/** The tables column: operators, pens, combo (columns, rows shown). */
export const LT_TABLES = {
  operators: { columns: 10, rows: 5 },
  pens: { columns: 10, rows: 10 },
  combo: { columns: 4, rows: 3 },
} as const;
/** The rail: activity feed, attention, verification (rows shown). */
export const LT_RAIL_ROWS = { activity: 6, attention: 2, verification: 3 } as const;
