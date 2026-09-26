"use client";

/**
 * The bell's READ path: a plain GET to `app/api/notifications/route.ts`. Not a Server Action --
 * an action POST re-renders the page server-side and, on a page that `redirect()`s, crashed the
 * Router with a late state update. A fetch renders nothing and a badge poll costs one API read.
 * The envelope is the same `NotificationFeedActionResult` the mark-read action family uses.
 */

import type { NotificationFeedActionResult } from "./notification-actions";

export type NotificationBadgeResult = { ok: true; unreadCount: number } | { ok: false; code: string };

export async function fetchNotificationFeed(cursor?: string, signal?: AbortSignal): Promise<NotificationFeedActionResult> {
  return fetchPage({ cursor }, signal);
}

/**
 * The badge poll: the smallest page the feed allows (limit=1). There is no count-only endpoint;
 * `unread_count` is whole-feed regardless of page size, so one row is the cheapest exact answer.
 */
export async function fetchNotificationBadge(signal?: AbortSignal): Promise<NotificationBadgeResult> {
  const result = await fetchPage({ limit: 1 }, signal);
  return result.ok ? { ok: true, unreadCount: result.feed.unread_count } : result;
}

async function fetchPage(params: { cursor?: string; limit?: number }, signal?: AbortSignal): Promise<NotificationFeedActionResult> {
  const search = new URLSearchParams();
  if (params.cursor) search.set("cursor", params.cursor);
  if (params.limit) search.set("limit", String(params.limit));
  const query = search.size ? `?${search.toString()}` : "";
  const response = await fetch(`/api/notifications${query}`, { cache: "no-store", credentials: "same-origin", signal });
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    return { ok: false, code: response.ok ? "invalid_response" : `http_${response.status}` };
  }
  if (body && typeof body === "object" && "ok" in body) {
    const result = body as NotificationFeedActionResult;
    if (result.ok && result.feed && Array.isArray(result.feed.items)) return result;
    if (!result.ok && typeof result.code === "string") return result;
  }
  return { ok: false, code: `http_${response.status}` };
}
