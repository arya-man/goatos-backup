import "server-only";

/**
 * The feature's server-side read + write, re-exported from `lib/api/notifications-server` -- see
 * the note there for why the read lives in `lib/api` (the GET route handler that serves the bell
 * must import it, and route handlers do not deep-import features).
 */
export { getInAppNotificationFeed, postInAppNotificationsRead } from "@/lib/api/notifications-server";
