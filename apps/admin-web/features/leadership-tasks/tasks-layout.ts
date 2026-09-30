// Layout constants shared by /tasks, its loading.tsx and its UrlSuspense fallback. The toolbar and the
// board read the same values (guard: tasks-loading-mirror).
import { PEOPLE_DROPDOWN_WIDTH } from "@/components/people-dropdown";
import { TASK_BOARD_COLUMNS } from "./task-url";

/** The scope select's min width and the sort select's width from md up. */
export const TASK_SCOPE_WIDTH = 180;
export const TASK_SORT_WIDTH = 180;
/** The card toolbar's fields in order: scope, assignee and raised-by dropdowns, sort, then search. */
export const TASK_TOOLBAR_FIELDS: (number | "search")[] = [TASK_SCOPE_WIDTH, PEOPLE_DROPDOWN_WIDTH, PEOPLE_DROPDOWN_WIDTH, TASK_SORT_WIDTH, "search"];
/** The "Dates: any" disclosure button's min width beside the search. */
export const TASK_DATES_BUTTON_WIDTH = 134;
/** Status tabs while loading: the contract's filters are All, one per board column, and Overdue. */
export const TASK_STATUS_TAB_COUNT = TASK_BOARD_COLUMNS.length + 2;
/** Placeholder cards per board lane while loading (the lanes are the board's status columns). */
export const TASK_BOARD_SKELETON_LANES = TASK_BOARD_COLUMNS.map((_, i) => 3 - (i % 2));
/**
 * The action slot's "No one can be given a task from here yet" note (NewTaskModal with no assignable
 * people; DECIDED no dead "New task"): one 44px line beside the toggle from md, its own full row below md.
 * It is the rare state (real users almost always have assignees), so the loading twin does NOT mirror it.
 */
export const TASK_NO_ASSIGNEES_NOTE_HEIGHT = 44;
export const TASK_NO_ASSIGNEES_NOTE_BASIS = { xs: "100%", md: "auto" } as const;
/**
 * Header actions while loading: the NORMAL state (coordinator decision, FIXJ6): the board / list
 * ToggleButtonGroup (73x40, 101x54 with 44px phone taps), then the 108x36 "New task" button.
 */
export const TASK_HEADER_ACTION_WIDTHS = [{ xs: 101, md: 73 }, 108];
export const TASK_HEADER_ACTION_HEIGHTS = [{ xs: 54, md: 40 }, undefined];
