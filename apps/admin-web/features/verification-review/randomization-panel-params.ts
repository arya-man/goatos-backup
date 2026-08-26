// Server-safe module ON PURPOSE — do not add "use client" here, and do not move these constants
// into randomization-panel.tsx.
//
// A constant exported from a "use client" module and imported by a Server Component arrives as a
// client-reference proxy, not the string. Nothing throws, which is what makes it dangerous: the
// CLIENT hook reads `?vi_randomization=open` from the URL correctly and opens the drawer, while the
// SERVER-built closeHref fails to delete a key it never matched — so closing writes a URL that
// still says open and the hook immediately reopens from it.
//
// Same trap, same fix as analytics-panel-params.ts, video-log-params.ts and actions-date-params.ts.

/** Selection key + id for the randomization overlay (`#vi_randomization=open`). */
export const RANDOMIZATION_PANEL_SELECTION_KEY = "vi_randomization";
export const RANDOMIZATION_PANEL_ID = "open";
