// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** Rows per page when the contract offers no page sizes. */
export const DEFAULT_PAGE_SIZE = 20;
/** Request status tabs, in order ("" = all). */
export const STATUS_FILTERS = ["", "pending", "approved", "rejected", "withdrawn"] as const;
/** The status tiles' Grid item size (four CourseWidgetSummary tiles on a spacing-3 Grid). */
export const LEAVE_TILE_SIZE = { xs: 12, sm: 6, md: 3 };
/** The leave toolbar's fields, in order: park and designation selects (160), the date pair, then search. */
export const LEAVE_TOOLBAR_FIELDS: (number | "search")[] = [160, 160, 246, 246, "search"];
