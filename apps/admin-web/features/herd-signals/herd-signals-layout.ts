import { FILTER_SELECT_MIN } from "@/components/app/filter-field-widths";
// Layout constants shared by HerdSignalsFilters and its loading twin (herd-signals-skeletons.tsx), so
// the skeleton's filter card cannot drift from the page's.
/** The search field's flex (it takes the row's spare width, 240 minimum from md). */
export const HERD_SIGNALS_SEARCH_SX = { flex: "1 1 280px", minWidth: { xs: 1, md: 240 } } as const;
/** Every select on the bar: the WorklistFilters select floor from sm, full width in the phone drawer. */
export const HERD_SIGNALS_SELECT_MIN = FILTER_SELECT_MIN;
/** The md+ bar on the Live tab, in order: search, pen, movement (the rest sit behind "More"). */
export const HERD_SIGNALS_BAR_FIELDS = ["search", HERD_SIGNALS_SELECT_MIN, HERD_SIGNALS_SELECT_MIN] as ("search" | number)[];
/** The live table's columns (the Live tag signals contract columns). */
export const HERD_SIGNALS_LIVE_COLUMNS = 21;

// Loading-twin estimates of rendered sizes the page does not set itself (copy-driven), measured on the
// served page at 1440 and 390.
/** KPI detail captions: one line on a phone, two at four across; the keys below wrap to two lines even full width. */
export const HERD_SIGNALS_KPI_CAPTION_LINES = { xs: 1, md: 2 } as const;
export const HERD_SIGNALS_KPI_WRAPPED_KEYS: readonly string[] = ["moving_now"];
export const HERD_SIGNALS_KPI_WRAPPED_LINES = 2;
/** The header's live-stream control width. */
export const HERD_SIGNALS_HEADER_ACTION_WIDTHS = [150];
