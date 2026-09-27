// Layout constants shared by /routines and its loading.tsx, so the skeleton cannot drift from the page.
/** The Today tiles' Grid item size (five CourseWidgetSummary tiles on a spacing-3 Grid). */
export const ROUTINE_TILE_SIZE = { xs: 12, sm: 6, md: 4, lg: "grow" } as const;
/** Toolbar select width from sm up; RoutinesToolbarRow and the skeleton read it. */
export const ROUTINES_SELECT_WIDTH = 160;
const S = ROUTINES_SELECT_WIDTH;
/** The routines card toolbar: park, status and role selects, then search. */
export const ROUTINES_TOOLBAR_FIELDS: (number | "search")[] = [S, S, S, "search"];
/** The tasks card toolbar: routine, state and assignee selects, the day pair on its own row (502 each at 1440), then search. */
export const TASKS_TOOLBAR_FIELDS: (number | "search")[] = [S, S, S, 502, 502, "search"];
/** Both table cards' CardHeader padding (the page and its skeleton share it). */
export const ROUTINES_CARD_HEADER_SX = { px: 3, pt: 2.5, pb: 1.5, alignItems: "center", gap: 1.5, flexWrap: "wrap" } as const;
