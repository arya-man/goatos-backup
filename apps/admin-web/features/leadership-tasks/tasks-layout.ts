// Layout constants shared by /tasks, its loading.tsx and its UrlSuspense fallback.
import { TASK_BOARD_COLUMNS } from "./task-url";

/** The card toolbar's fields in order: scope (180), assignee and raised-by (200), sort (180), then search. */
export const TASK_TOOLBAR_FIELDS: (number | "search")[] = [180, 200, 200, 180, "search"];
/** The "Dates: any" disclosure button beside the search. */
export const TASK_DATES_BUTTON_WIDTH = 134;
/** Placeholder cards per board lane while loading (the lanes are the board's status columns). */
export const TASK_BOARD_SKELETON_LANES = TASK_BOARD_COLUMNS.map((_, i) => 3 - (i % 2));
/** Header actions: the board / list ToggleButtonGroup (73x40, 101x54 with 44px phone taps) and New task. */
export const TASK_HEADER_ACTION_WIDTHS = [{ xs: 101, md: 73 }, 108];
export const TASK_HEADER_ACTION_HEIGHTS = [{ xs: 54, md: 40 }, undefined];
