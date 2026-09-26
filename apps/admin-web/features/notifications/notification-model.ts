/**
 * The in-app notification centre's vocabulary, as pure functions with NO imports.
 *
 * Import-free on purpose so `notification-model.test.mjs` can exercise it under `node --test`
 * without the `@/` path alias (the same reason `features/leadership-tasks/task-url.ts` is), and
 * because every rule here is a rule the READER sees: what order the list is in, which row still
 * counts as unread, and where a row sends the browser.
 *
 * The row shape mirrors the `notification_requests` table one-for-one (notification_request_id,
 * notification_type, title, body, status, requested_at, read_at, context) rather than inventing a
 * frontend model, so the read endpoint that does not exist yet can be a straight projection of
 * the rows already being written by the notification bridge.
 */

/**
 * `context` as the leadership-task notify consumer writes it. Every field is optional because the
 * jsonb is an OPEN key space per notification type, and a centre that threw on an unfamiliar key
 * would break the moment a new type ships.
 *
 * `href` is deliberately absent: the one the bridge writes ("/leadership-tasks/<id>") is the
 * ANDROID route and would 404 in the browser. The web destination is derived from `task_id`.
 */
export type NotificationContext = {
  task_id?: string;
  task_no?: string;
  screen?: string;
  group_key?: string;
  priority?: string;
  message_key?: string;
  target?: string;
  status?: string;
  /** Procurement load alerts (`procurement_load_overdue`): the load and its human ref. */
  load_id?: string;
  load_ref?: string;
  farm?: string;
  vendor_name?: string;
};

export type InAppNotification = {
  notification_request_id: string;
  /** Open key space (`leadership_task_raised`, `leadership_task_done`, mention/comment types in
   *  flight). An unknown type renders its backend title and body, and links by `context.task_id`. */
  notification_type: string;
  /** Backend-owned headline: WHAT happened. The frontend composes no notification prose. */
  title: string;
  /** Backend-owned detail line. */
  body: string;
  /** The table's own status; `read` is the one this UI treats as read. */
  status: string;
  /** RFC3339. WHEN it happened; the sort key. */
  requested_at: string;
  read_at?: string | null;
  /** WHO did it, as the backend resolved it. Absent for system-raised rows. */
  actor_name?: string;
  context?: NotificationContext;
};

export type NotificationFeed = {
  items: InAppNotification[];
  /** The caller's unread total, which can exceed the page of rows served. */
  unread_count: number;
  /** The caller's TOTAL row count, only when the endpoint serves one; the page size is never a total. */
  total_count?: number;
  /** Keyset cursor for an older page, when the endpoint serves one. */
  next_cursor?: string;
  /**
   * Whether browser push can work on THIS stack, decided server-side (push key served, web
   * sign-in configured, not local bearer mode). False or absent = the ⚙ section is not rendered
   * at all; nothing explains why, because the reason is never a person's business.
   */
  push_available?: boolean;
  /**
   * False while the read endpoint is not deployed (the UI treats a 404/501 as "not wired yet"),
   * so the panel shows its empty state instead of an error card on every page load.
   */
  available: boolean;
};

export const EMPTY_NOTIFICATION_FEED: NotificationFeed = { items: [], unread_count: 0, available: false };

/** The status value that means the reader has seen it. */
export const NOTIFICATION_STATUS_READ = "read";

/** How high the bell badge counts before it says "more than". */
export const NOTIFICATION_BADGE_MAX = 9;

/** The panel serves one bounded page; the bell is a glance, not a mailbox. */
export const NOTIFICATION_PAGE_LIMIT = 20;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isNotificationTaskId(value: string | undefined): boolean {
  return typeof value === "string" && UUID.test(value.trim());
}

export function isNotificationRead(item: InAppNotification): boolean {
  return item.status === NOTIFICATION_STATUS_READ || Boolean((item.read_at ?? "").trim());
}

/**
 * Where a leadership-task notification sends the reader.
 *
 * `team_progress` is the ONE scope that can show a task the reader neither raised nor owns — a
 * mention reaches bystanders by design — so a mention link that landed on `assigned_to_me` would
 * show an empty list for a task that plainly exists. The id travels as `task` because that is the
 * parameter the Tasks desk already reads.
 */
export function leadershipTaskNotificationHref(taskId: string): string {
  return `/tasks?scope=team_progress&task=${encodeURIComponent(taskId.trim())}`;
}

/**
 * The row's destination, or undefined for a row that refers to nothing openable in admin-web.
 *
 * Derived from `context.task_id`, never from `context.href` (that is the Android route). A row
 * whose task id is not a uuid gets NO link rather than a guessed one: a notification row carries
 * attacker-influenced content, and a bad link on a CEO's bell is a phishing surface.
 */
export function notificationHref(item: InAppNotification): string | undefined {
  const taskId = item.context?.task_id;
  if (isNotificationTaskId(taskId)) return leadershipTaskNotificationHref(taskId!.trim());
  return undefined;
}

export type NotificationAction = { kind: "task" | "load" | "screen"; href: string };

/**
 * Backend `context.screen` keys that have an admin-web desk. A FIXED map, never the raw
 * `context.href` (that is the Android route, and a row carries attacker-influenced content).
 */
const SCREEN_ROUTES: Readonly<Record<string, { kind: "load" | "screen"; href: string }>> = {
  sales_loads: { kind: "load", href: "/sales/loads" },
  feed_stock: { kind: "screen", href: "/feed/analytics?tab=items" },
  feed_direction: { kind: "screen", href: "/feed/direction" },
  verification: { kind: "screen", href: "/verify" },
};

/**
 * The ONE inline action a row can carry, or undefined. Only where the backend gives something
 * openable in admin-web: a leadership task (by `task_id`, see `notificationHref`) or a
 * procurement load (`load_id` -> the loads desk, which reads `load`). No other type gets a
 * button; a guessed link on a CEO's bell is a phishing surface.
 */
export function notificationAction(item: InAppNotification): NotificationAction | undefined {
  const task = notificationHref(item);
  if (task) return { kind: "task", href: task };
  const loadId = (item.context?.load_id ?? "").trim();
  if (loadId && UUID.test(loadId)) return { kind: "load", href: `/sales/loads?load=${encodeURIComponent(loadId)}` };
  const screen = SCREEN_ROUTES[(item.context?.screen ?? "").trim()];
  return screen ? { ...screen } : undefined;
}

const CHIP_KEYS: ReadonlyArray<keyof NotificationContext> = ["task_no", "load_ref", "priority", "status", "farm"];
const CHIP_MAX = 3;

/** Short backend-owned context values worth a chip (task no, load ref, priority...), at most three. */
export function notificationChips(item: InAppNotification): string[] {
  const out: string[] = [];
  for (const key of CHIP_KEYS) {
    const value = String(item.context?.[key] ?? "").trim();
    if (!value || value.length > 24 || out.includes(value)) continue;
    // "normal" priority is the default on every row; a chip that says nothing is noise.
    if (key === "priority" && value.toLowerCase() === "normal") continue;
    out.push(value);
    if (out.length >= CHIP_MAX) break;
  }
  return out;
}

/** Two-letter initials for the actor avatar, or "" when the row is system-raised. */
export function notificationInitials(actorName: string | undefined): string {
  const parts = (actorName ?? "").trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "";
  const first = parts[0][0] ?? "";
  const last = parts.length > 1 ? parts[parts.length - 1][0] ?? "" : "";
  return (first + last).toUpperCase();
}

/** Newest first, with a stable id tiebreak so equal timestamps never reshuffle between renders. */
export function sortNotificationsNewestFirst(items: InAppNotification[]): InAppNotification[] {
  return [...items].sort((left, right) => {
    if (left.requested_at === right.requested_at) {
      return left.notification_request_id < right.notification_request_id
        ? -1
        : left.notification_request_id > right.notification_request_id
          ? 1
          : 0;
    }
    return left.requested_at < right.requested_at ? 1 : -1;
  });
}

export function unreadNotifications(items: InAppNotification[]): InAppNotification[] {
  return items.filter((item) => !isNotificationRead(item));
}

/**
 * What the badge shows.
 *
 * The caller's server total wins when it is larger than the page of rows served, because the page
 * is capped at `NOTIFICATION_PAGE_LIMIT` and a badge that shrank on open would be a lie. Rows
 * this browser has just marked read are subtracted from BOTH sides, so the count falls by one per
 * click without waiting for a refetch.
 */
export function notificationBadgeCount(feed: NotificationFeed, locallyRead: readonly string[] = []): number {
  const readSet = new Set(locallyRead);
  const stillUnread = feed.items.filter(
    (item) => !isNotificationRead(item) && !readSet.has(item.notification_request_id),
  ).length;
  const cleared = feed.items.filter(
    (item) => !isNotificationRead(item) && readSet.has(item.notification_request_id),
  ).length;
  const serverUnread = Number.isFinite(feed.unread_count) ? Math.max(0, Math.trunc(feed.unread_count)) : 0;
  return Math.max(stillUnread, serverUnread - cleared, 0);
}

/**
 * Applies the rows this browser has already marked read.
 *
 * Mark-as-read is optimistic: the row goes quiet the instant it is clicked and the server action's
 * answer arrives later. The locally-read set is the single place that optimism lives, so a refresh
 * that still reports the row unread cannot make it flash back to bold.
 */
export function applyLocallyRead(feed: NotificationFeed, locallyRead: readonly string[]): NotificationFeed {
  const readSet = new Set(locallyRead);
  return {
    ...feed,
    items: sortNotificationsNewestFirst(feed.items).map((item) =>
      readSet.has(item.notification_request_id) && !isNotificationRead(item)
        ? { ...item, status: NOTIFICATION_STATUS_READ }
        : item,
    ),
    unread_count: notificationBadgeCount(feed, locallyRead),
  };
}

/** The ids a "mark all as read" click must send: every row on the page still unread. */
export function idsToMarkAllRead(feed: NotificationFeed, locallyRead: readonly string[] = []): string[] {
  const readSet = new Set(locallyRead);
  return feed.items
    .filter((item) => !isNotificationRead(item) && !readSet.has(item.notification_request_id))
    .map((item) => item.notification_request_id);
}

/** Drops blanks and duplicates from a mark-read request; an empty id is not a request. */
export function normalizeNotificationIds(ids: readonly string[]): string[] {
  const out: string[] = [];
  for (const id of ids) {
    const trimmed = (id ?? "").trim();
    if (!trimmed || out.includes(trimmed)) continue;
    out.push(trimmed);
  }
  return out;
}

/**
 * Groups a page by `context.group_key` while keeping newest-first order inside each group.
 *
 * The bridge already stamps `group_key` ("leadership_tasks"), and the panel uses it as the row's
 * section so a future type (health, procurement) lands in its own block without a UI change.
 */
export function notificationGroupKey(item: InAppNotification): string {
  const key = (item.context?.group_key ?? "").trim();
  return key || item.notification_type;
}
