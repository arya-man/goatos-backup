// Server-safe module ON PURPOSE — do not add "use client" here, and do not move these constants
// back into analytics-panel.tsx.
//
// A constant exported from a "use client" module and imported by a Server Component arrives as a
// client-reference proxy, not the string. Nothing throws, which is what makes it dangerous: the
// CLIENT hook reads `?vi_analytics=open` from the URL correctly and opens the drawer, while the
// SERVER-built closeHref fails to delete a key it never matched — so closing wrote a URL that still
// said open and the hook immediately reopened from it. The analytics drawer could not be closed
// from a deep link at all.
//
// Same trap, same fix as video-log-params.ts and actions-date-params.ts.

/** Selection key + id for the analytics overlay (`#vi_analytics=open`). */
export const ANALYTICS_PANEL_SELECTION_KEY = "vi_analytics";
export const ANALYTICS_PANEL_ID = "open";
