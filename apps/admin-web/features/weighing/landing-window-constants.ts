/** Both Weights pages carry the window in the same two parameters, so a link survives moving between them. */
export const WINDOW_FROM_PARAM = "wt_from";
export const WINDOW_TO_PARAM = "wt_to";

/**
 * The farm asked for the default period to start at the first dense/proper weighing history.
 * The shared staging data currently has the reliable run from 2026-08-03 onward; before that,
 * July rows are sparse weekly checks and make the default read noisy. Keep this fixed until enough
 * later history exists to replace it with a true long-term rolling window.
 */
export const DEFAULT_WINDOW_FROM = "2026-08-03";

/**
 * Earliest day the Weights calendars allow picking. Weighing history in the product starts
 * August 2026; earlier days would only ever return an empty span, so the calendar
 * disables them rather than offering a request that answers nothing.
 */
export const WINDOW_MIN_DATE = "2026-08-01";

export const LATEST_LUMP_LOOKBACK_DAYS = 400;
export const BUSINESS_DAY = /^\d{4}-\d{2}-\d{2}$/;
