// Server-safe module ON PURPOSE — do not add "use client" here, and do not move these constants
// back into video-log-panel.tsx.
//
// A constant exported from a "use client" module and imported by a Server Component arrives as a
// client-reference PROXY, not the string. Nothing throws: `one(sp, VIDEO_LOG_DATE_KEY)` simply
// never matches, so the day selection silently fell back to today and every shed link opened the
// summary again. That defect shipped once here and is the same one actions-date-params.ts exists to
// prevent for the queue's own date params.

/** Query key carrying the day the Video Log panel is showing. Absent means today. */
export const VIDEO_LOG_DATE_KEY = "vl_date";

/** Query key carrying the operational location whose detail is open, as the backend's shed_key. */
export const VIDEO_LOG_SHED_KEY = "vl_shed";

/**
 * Placeholder the page puts in the shed href template for the Video Log component to substitute.
 *
 * Deliberately made only of characters URLSearchParams leaves untouched, so the token survives href
 * construction intact and a plain string replace finds it.
 */
export const VIDEO_LOG_SHED_TOKEN = "__VLSHED__";

/** Selection key + id for the panel overlay itself (`#vi_video_log=open`). */
export const VIDEO_LOG_PANEL_SELECTION_KEY = "vi_video_log";
export const VIDEO_LOG_PANEL_ID = "open";

/**
 * Park filter INSIDE the panel.
 *
 * Deliberately its own key rather than the page's top-bar park scope. The Scope Chrome Rule keeps
 * park scope in the top bar, and this does not replace it -- it narrows WITHIN whatever the top bar
 * already selected, so a reader can look at one park's arrivals without changing the scope of the
 * queue behind the drawer. See the note on this in VideoLog.
 */
export const VIDEO_LOG_PARK_KEY = "vl_park";

/** Free-text filter over the rows the day already returned. */
export const VIDEO_LOG_QUERY_KEY = "vl_q";
