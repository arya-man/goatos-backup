import "server-only";

/**
 * The ONE server-only read (plus its mark-read write) behind the in-app notification centre.
 *
 * It lives here, not in `lib/api/server.ts`, because that module is being edited concurrently;
 * it follows `lib/api/alerts-server.ts` line for line — same `ApiResult` envelope, same
 * `apiClientOptions`/`getServerConfig` helpers, no client fetch and no route handler.
 *
 * THE ENDPOINT DOES NOT EXIST YET. `notification_requests` is written by the notification bridge
 * and drained by the dispatcher worker; nothing serves it to the web. The paths below are the
 * proposal the coordinator should have the backend implement, and the type is a straight
 * projection of the table's columns so it can be built to match. Until it lands, the backend
 * answers 404 and `getInAppNotificationFeed` returns `available: false` — the panel then shows
 * its empty state rather than an error card on every page load, which is the right behaviour for
 * a bell that also has to survive the endpoint being disabled per tenant.
 */
import { createAppApiClient } from "@goatos/api-client";
import type { AppApiPaths } from "@goatos/api-client";
import { apiClientOptions, compactQuery, getServerConfig, request, type ApiResult } from "@/lib/api/server";
import {
  EMPTY_NOTIFICATION_FEED,
  NOTIFICATION_PAGE_LIMIT,
  normalizeNotificationIds,
  sortNotificationsNewestFirst,
  type InAppNotification,
  type NotificationFeed,
} from "./notification-model";

/** Proposed: GET one page of the caller's own durable notifications, newest first. */
const FEED_PATH = "/app/notifications" as keyof AppApiPaths & string;
/** Proposed: POST the ids the caller has read (sets `notification_requests.status = 'read'`). */
const READ_PATH = "/app/notifications/read" as keyof AppApiPaths & string;

/** The wire shape the endpoint must answer with. Column-for-column with the table. */
type NotificationFeedResponse = {
  items: InAppNotification[];
  unread_count: number;
  next_cursor?: string;
  trace_id?: string;
};

function notWired(error: { status?: number; code?: string; kind?: string }): boolean {
  // 404 = route not deployed yet; 501 = deployed but switched off. Both mean "no inbox", not
  // "something broke", and must not put an error card under a leader's bell on every page.
  return error.status === 404 || error.status === 501 || error.code === "not_found";
}

/**
 * One page of the caller's notifications.
 *
 * Scope and audience are the BACKEND's: a row exists in `notification_requests` only because the
 * bridge already routed it to this recipient, so this module forwards no audience filter of its
 * own and the caller can never ask for someone else's inbox.
 */
export async function getInAppNotificationFeed(params?: {
  cursor?: string;
  limit?: number;
}): Promise<ApiResult<NotificationFeed>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const limit = Math.max(1, Math.min(Math.trunc(params?.limit ?? NOTIFICATION_PAGE_LIMIT), 50));
  const result = await request(() =>
    client.request<NotificationFeedResponse>(FEED_PATH, {
      cache: "no-store",
      query: compactQuery({ cursor: params?.cursor, limit }),
    }),
  );
  if (!result.ok) {
    if (notWired(result.error)) return { ok: true, data: EMPTY_NOTIFICATION_FEED };
    return result;
  }
  const items = Array.isArray(result.data.items) ? result.data.items : [];
  return {
    ok: true,
    data: {
      items: sortNotificationsNewestFirst(items),
      unread_count: Number.isFinite(result.data.unread_count) ? Math.max(0, Math.trunc(result.data.unread_count)) : 0,
      next_cursor: result.data.next_cursor,
      available: true,
    },
  };
}

/**
 * Marks the given rows read.
 *
 * The id list is sent explicitly — never "everything before this timestamp" — so a row that
 * arrived while the panel was open is not silently swallowed by a mark-all the reader never saw.
 * The idempotency key is derived from the ids by the caller, so a double-click is one write.
 */
export async function postInAppNotificationsRead(
  notificationRequestIds: readonly string[],
  idempotencyKey: string,
): Promise<ApiResult<{ read_count: number }>> {
  const ids = normalizeNotificationIds(notificationRequestIds);
  if (ids.length === 0) return { ok: true, data: { read_count: 0 } };
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const result = await request(() =>
    client.request<{ read_count: number }>(READ_PATH, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body: { notification_request_ids: ids },
    }),
  );
  if (!result.ok && notWired(result.error)) return { ok: true, data: { read_count: 0 } };
  return result;
}
