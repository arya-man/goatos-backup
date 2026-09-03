// Client-safe window bounds for the Weights screens.
//
// This module exists because the export drawer is a CLIENT component and landing-window.ts is
// server-only (it imports the SSR API reader for the landing-window resolution). A client import
// of landing-window drags "server-only" into the browser bundle and the whole route 500s at
// compile time — so the one constant both sides need lives here, and landing-window re-exports it
// for its server-side callers.

/**
 * Earliest day the Weights calendars allow picking. Weighing history in Goat OS starts
 * August 2026; earlier days would only ever return an empty span, so the calendar
 * disables them rather than offering a request that answers nothing.
 */
export const WINDOW_MIN_DATE = "2026-08-01";
