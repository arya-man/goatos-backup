// Layout constants shared by /feed/direction, /feed/packing, their loading.tsx and UrlSuspense fallbacks.
/** The day's two KpiWidget tiles (pens, blocked) on a spacing-3 Grid. */
export const FEED_KPI_SIZE = { xs: 12, sm: 6 } as const;
/** The WorklistFilters fields: the day DatePicker (246), then park / pen / session selects (160). */
export const FEED_DIRECTION_FILTER_FIELDS = [246, 160, 160, 160];
/** Packing filters: the day DatePicker, then the park select. */
export const FEED_PACKING_FILTER_FIELDS = [246, 160];
/** The sheet / worklist table columns while loading (the contract's tables carry eight). */
export const FEED_TABLE_COLUMNS = 8;
/** Sheet / worklist rows per page when the contract offers no page sizes. */
export const FEED_DEFAULT_PAGE_SIZE = 10;
