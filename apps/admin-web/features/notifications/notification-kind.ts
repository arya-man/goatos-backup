/**
 * How a notification row LOOKS, derived from the backend's `notification_type` — pure, import-free
 * (like `notification-model.ts`) so `notification-kind.test.mjs` runs it under `node --test`.
 *
 * The backend owns the words (title, body); this module owns only the semantic dressing: which
 * icon sits in the tinted circle, which tone tints it, and the one-word category on the meta line.
 * The type key space is OPEN (backend/internal/notificationbridge/*, `NotificationType*`
 * constants), so an unknown type falls back to a neutral bell rather than throwing or inventing a
 * state. Nothing here reads `status`: read/unread stays `isNotificationRead`'s job.
 */

export type NotificationTone = "primary" | "info" | "success" | "warning" | "error" | "violet" | "neutral";

/** Icon NAMES, not components, so this file stays import-free; the panel maps names to registered Iconify icons. */
export type NotificationIconName =
  | "bell"
  | "clock"
  | "check"
  | "at-sign"
  | "message"
  | "clipboard"
  | "heart-pulse"
  | "wheat"
  | "truck"
  | "scale"
  | "calendar"
  | "alert"
  | "user";

export type NotificationKind = {
  tone: NotificationTone;
  icon: NotificationIconName;
  /** One word for the meta line ("2h ago · Tasks"). */
  category: string;
};

const FALLBACK: NotificationKind = { tone: "neutral", icon: "bell", category: "Update" };

/**
 * Prefix rules, first match wins. Written as prefixes because the bridge names types
 * `<domain>_<event>` (`leadership_task_raised`, `feed_low_stock`, `procurement_load_overdue`).
 */
const RULES: ReadonlyArray<readonly [test: (type: string) => boolean, kind: NotificationKind]> = [
  // Leadership tasks — the one family that links somewhere in admin-web today.
  [(t) => t === "leadership_task_mentioned", { tone: "violet", icon: "at-sign", category: "Mention" }],
  [(t) => t === "leadership_task_commented", { tone: "info", icon: "message", category: "Comment" }],
  [(t) => t === "leadership_task_done", { tone: "success", icon: "check", category: "Tasks" }],
  [(t) => t.startsWith("leadership_task"), { tone: "primary", icon: "clipboard", category: "Tasks" }],
  // Approvals: decisions are brand-toned, requests waiting on the reader are amber.
  [(t) => t.endsWith("_decided"), { tone: "primary", icon: "check", category: "Approval" }],
  [(t) => t.startsWith("leave_request"), { tone: "warning", icon: "user", category: "Leave" }],
  [(t) => t.startsWith("animal_purchase"), { tone: "warning", icon: "truck", category: "Purchases" }],
  // Ageing / overdue / due: amber clock.
  [(t) => t.includes("overdue") || t.includes("_due") || t.includes("due_"), { tone: "warning", icon: "clock", category: "Due" }],
  [(t) => t.includes("reminder") || t === "nudge", { tone: "warning", icon: "clock", category: "Reminder" }],
  // Health: red.
  [(t) => t.startsWith("health") || t.includes("diagnosis") || t.includes("toxin"), { tone: "error", icon: "heart-pulse", category: "Health" }],
  [(t) => t === "escalation" || t.includes("missed"), { tone: "error", icon: "alert", category: "Escalation" }],
  // Feed & stock.
  [(t) => t.startsWith("feed_"), { tone: "success", icon: "wheat", category: "Feed" }],
  [(t) => t.startsWith("weighing"), { tone: "info", icon: "scale", category: "Weighing" }],
  [(t) => t.startsWith("pen_"), { tone: "info", icon: "calendar", category: "Pens" }],
  [(t) => t.includes("verification") || t === "rework", { tone: "info", icon: "clipboard", category: "Verification" }],
  [(t) => t.startsWith("market_survey"), { tone: "info", icon: "clipboard", category: "Market" }],
];

export function notificationKind(notificationType: string | undefined | null): NotificationKind {
  const type = (notificationType ?? "").trim().toLowerCase();
  if (!type) return FALLBACK;
  for (const [test, kind] of RULES) if (test(type)) return kind;
  return FALLBACK;
}

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * "just now" / "5m ago" / "3h ago" / "2d ago" for the meta line. Anything older than a week, an
 * unparseable stamp, or a stamp in the future returns "" so the caller falls back to the absolute
 * `DD/MM/YYYY HH:MM` the rest of the console prints — the row never shows "in 3 hours".
 */
export function relativeNotificationTime(iso: string, now: number = Date.now()): string {
  const at = Date.parse(iso);
  if (!Number.isFinite(at)) return "";
  const delta = now - at;
  if (delta < 0) return "";
  if (delta < MINUTE) return "just now";
  if (delta < HOUR) return `${Math.floor(delta / MINUTE)}m ago`;
  if (delta < DAY) return `${Math.floor(delta / HOUR)}h ago`;
  if (delta < 7 * DAY) return `${Math.floor(delta / DAY)}d ago`;
  return "";
}

export type NotificationTab = "all" | "unread" | "archived";

/**
 * The tab counts the header chips show. Unread never reports fewer than the server's own total.
 * All shows the server TOTAL when the feed carries one and nothing otherwise -- the loaded page
 * size is not a count of anything a reader cares about, and "All 20 · Unread 4203" is a lie.
 */
export function notificationTabCounts(
  items: ReadonlyArray<{ status: string; read_at?: string | null }>,
  serverUnread: number,
  serverTotal?: number,
): { all: number | undefined; unread: number; archived: number | undefined } {
  const unreadOnPage = items.filter((item) => item.status !== "read" && !(item.read_at ?? "").trim()).length;
  const server = Number.isFinite(serverUnread) ? Math.max(0, Math.trunc(serverUnread)) : 0;
  const unread = Math.max(unreadOnPage, server);
  const all = typeof serverTotal === "number" && Number.isFinite(serverTotal) ? Math.max(Math.trunc(serverTotal), items.length, unread) : undefined;
  // Archived = read. Its count is only honest when a total exists (total - unread); a count of
  // the read rows on the loaded page would be the page size in disguise.
  const archived = all === undefined ? undefined : Math.max(0, all - unread);
  return { all, unread, archived };
}
