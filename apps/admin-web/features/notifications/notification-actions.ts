"use server";

/**
 * The notification centre's two Server Actions.
 *
 * The bell lives in the shell, which is a Client Component, so the panel reaches the server-only
 * adapter through these rather than through a route handler: no browser-visible endpoint, no
 * second auth path, and the authenticated API adapter stays the only way out.
 */

import { postInAppNotificationsRead } from "./notification-feed-server";
import { isUsableIdempotencyKey, notificationsReadIdempotencyKey } from "./read-idempotency";
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
export type MarkNotificationsReadResult = { ok: true; readCount: number } | { ok: false; code: string };

/**
 * Mark the given rows read.
 *
 * The ids are validated here, not trusted from the browser: anything blank or duplicated is
 * dropped, an empty request is a no-op, and a page's worth is the cap so one call cannot be
 * turned into an unbounded write. The idempotency key is DERIVED from the id set, so the same
 * click twice — or a retried action — is one write, and a different set gets its own key.
 *
 * The key is a HASH of that set and not the ids themselves: the backend refuses a key over 200
 * characters, and concatenating the ids exceeded it from the fifth notification onward, which
 * made "Mark all read" a 400 that the optimistic state hid. See read-idempotency.ts.
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
  const idempotencyKey = notificationsReadIdempotencyKey(ids);
  // Mirrors the discipline the leadership-tasks actions in this same PR already apply to every
  // write. The hashed key is a constant 93 characters, so this can only fire if the derivation
  // is changed -- and then it fails HERE, loudly and before the optimistic UI has cleared
  // anything, rather than as a 400 the panel swallows.
  if (!isUsableIdempotencyKey(idempotencyKey)) return { ok: false, code: "invalid_idempotency_key" };
  const result = await postInAppNotificationsRead(ids, idempotencyKey);
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind };
  return { ok: true, readCount: result.data.read_count };
}
