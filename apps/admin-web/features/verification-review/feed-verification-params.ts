// Server-safe module ON PURPOSE -- do not add "use client" here. A constant exported from a
// "use client" module arrives in a Server Component as a client-reference PROXY, not the string, so
// `one(sp, KEY)` silently never matches (see video-log-params.ts, where that defect shipped once).

/** Selection key + id for the Feed Verification panel overlay (`#vi_feed_verify=open`). */
export const FEED_VERIFICATION_PANEL_SELECTION_KEY = "vi_feed_verify";
export const FEED_VERIFICATION_PANEL_ID = "open";

/** Query key carrying the FEED day the panel shows (the day the animals eat). Absent means today. */
export const FEED_VERIFICATION_DATE_KEY = "fv_date";

/**
 * Park narrowing INSIDE the panel. Its own key, not the top-bar park scope: it narrows within
 * whatever the top bar already selected, without reshaping the queue behind the drawer.
 */
export const FEED_VERIFICATION_PARK_KEY = "fv_park";

/** Placeholder the page puts in the park href template; only URL-safe characters, so it survives. */
export const FEED_VERIFICATION_PARK_TOKEN = "__FVPARK__";
