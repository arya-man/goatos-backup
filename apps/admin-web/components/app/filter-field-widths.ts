// Filter control width floors, shared by the controls themselves (WorklistFilters, DateRangePicker)
// and the loading twins that stand in for them (features/**/*-layout.ts), so a skeleton's filter row
// cannot drift from the bar it twins. Plain module (no "use client"): server skeletons import it too.
/** A WorklistFilters select / multi-select, from sm. */
export const FILTER_SELECT_MIN = 160;
/** A WorklistFilters single DatePicker, from sm. */
export const FILTER_DATE_MIN = 180;
/** The DateRangePicker trigger field, from sm. */
export const DATE_RANGE_PICKER_MIN = 300;
/**
 * The FilterBar search's flex basis. A folded bar (below md: search, the Filters button, the ⋮ actions)
 * uses the small phone basis so the ⋮ stays on the search row instead of wrapping alone onto a second
 * row (TR3-P1-1; guard: filter-bar-fold-one-row). FilterCardSkeleton `fold` reads the same value.
 */
export const FILTER_SEARCH_BASIS = 240;
export const FILTER_SEARCH_FOLD_BASIS = { xs: 120, md: FILTER_SEARCH_BASIS } as const;
