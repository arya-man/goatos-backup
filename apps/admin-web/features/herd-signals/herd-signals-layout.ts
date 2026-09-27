// Layout constants shared by HerdSignalsFilters and its loading twin (herd-signals-skeletons.tsx), so
// the skeleton's filter card cannot drift from the page's.
/** The search field's flex (it takes the row's spare width, 240 minimum from md). */
export const HERD_SIGNALS_SEARCH_SX = { flex: "1 1 280px", minWidth: { xs: 1, md: 240 } } as const;
/** Every select on the bar: at least 160 wide from sm, full width in the phone drawer. */
export const HERD_SIGNALS_SELECT_MIN = 160;
/** The md+ bar on the Live tab, in order: search, pen, movement (the rest sit behind "More"). */
export const HERD_SIGNALS_BAR_FIELDS = ["search", HERD_SIGNALS_SELECT_MIN, HERD_SIGNALS_SELECT_MIN] as ("search" | number)[];
