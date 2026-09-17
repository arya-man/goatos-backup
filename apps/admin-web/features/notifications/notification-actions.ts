"use server";

/**
 * The notification centre's two Server Actions.
 *
 * The bell lives in the shell, which is a Client Component, so the panel reaches the server-only
 * adapter through these rather than through a route handler: no browser-visible endpoint, no
 * second auth path, and the authenticated API adapter stays the only way out.
 */

import { getInAppNotificationFeed, postInAppNotificationsRead } from "./notification-feed-server";
import {
  NOTIFICATION_PAGE_LIMIT,
  normalizeNotificationIds,
  type NotificationFeed,
} from "./notification-model";

export type NotificationFeedActionResult =
  | { ok: true; feed: NotificationFeed }
  | { ok: false; code: string };

/**
 * Read one page for the bell.
 *
 * server-action-read-only: no write, so there is nothing to make idempotent.
 */
export async function loadNotificationFeedAction(cursor?: string): Promise<NotificationFeedActionResult> {
  const safeCursor = typeof cursor === "string" && cursor.trim() ? cursor.trim().slice(0, 512) : undefined;
  const result = await getInAppNotificationFeed({ cursor: safeCursor, limit: NOTIFICATION_PAGE_LIMIT });
  if (!result.ok) {
    // The panel renders backend-owned copy for the code; it never composes a sentence from it.
    return { ok: false, code: result.error.code ?? result.error.kind };
  }
  return { ok: true, feed: result.data };
}

export type MarkNotificationsReadResult = { ok: true; readCount: number } | { ok: false; code: string };

/**
 * Mark the given rows read.
 *
 * The ids are validated here, not trusted from the browser: anything blank or duplicated is
 * dropped, an empty request is a no-op, and a page's worth is the cap so one call cannot be
 * turned into an unbounded write. The idempotency key is DERIVED from the id set, so the same
 * click twice — or a retried action — is one write, and a different set gets its own key.
 */
export async function markNotificationsReadAction(
  notificationRequestIds: readonly string[],
): Promise<MarkNotificationsReadResult> {
  if (!Array.isArray(notificationRequestIds)) return { ok: false, code: "invalid_request" };
  const ids = normalizeNotificationIds(notificationRequestIds.map((id) => String(id ?? ""))).slice(
    0,
    NOTIFICATION_PAGE_LIMIT,
  );
  if (ids.length === 0) return { ok: true, readCount: 0 };
  const idempotencyKey = `admin-web-notifications-read:${[...ids].sort().join(",")}`;
  const result = await postInAppNotificationsRead(ids, idempotencyKey);
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind };
  return { ok: true, readCount: result.data.read_count };
}
