// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** Rows per page when the contract offers no page sizes. */
export const DEFAULT_PAGE_SIZE = 20;
/** Request status tabs, in order ("" = all). */
export const STATUS_FILTERS = ["", "pending", "approved", "rejected", "withdrawn"] as const;
/** The status tiles' Grid item size (four CourseWidgetSummary tiles on a spacing-3 Grid). */
export const LEAVE_TILE_SIZE = { xs: 12, sm: 6, md: 3 };
/** The leave toolbar's select width from sm up (park, designation); the toolbar and its skeleton read it. */
export const LEAVE_SELECT_WIDTH = 160;
/** The leave toolbar's fields, in order: park and designation selects, the date pair (246 each at 1440), then search. */
export const LEAVE_TOOLBAR_FIELDS: (number | "search")[] = [LEAVE_SELECT_WIDTH, LEAVE_SELECT_WIDTH, 246, 246, "search"];
/** The queue card's CardHeader padding (the page and its skeleton share it). */
export const LEAVE_QUEUE_HEADER_SX = { pt: 2.5, px: 3, pb: 1.5, mb: 2, alignItems: "center" } as const;
/** The list card's CardHeader padding. */
export const LEAVE_LIST_HEADER_SX = { pt: 2.5, px: 3, pb: 1.5, alignItems: "center" } as const;
