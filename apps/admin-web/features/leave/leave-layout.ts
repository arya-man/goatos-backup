// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** Rows per page when the contract offers no page sizes. */
export const DEFAULT_PAGE_SIZE = 20;
/** Request status tabs, in order ("" = all). */
export const STATUS_FILTERS = ["", "pending", "approved", "rejected", "withdrawn"] as const;
