/**
 * The notification centre's copy, resolved from the backend bootstrap contract with a local
 * fallback per key.
 *
 * WHY A FALLBACK MAP LIVES HERE. Every visible string in admin-web comes from the backend
 * (`AdminWebBootstrapResponse.copy` / `AdminWebPageContract.copy`), and only ONE key for this
 * surface exists today: `top_bar.notifications.disabled_reason`. The centre needs about a dozen
 * more, and the backend cannot serve them until the contract change lands. The same shape as
 * `lib/admin-ui-contract.ts`'s `COPY_FALLBACKS` is used for the same reason: a frontend that
 * deploys one release ahead of the backend must not render blanks or throw. The moment the keys
 * below exist in the bootstrap contract they win, and this map can be deleted key by key.
 *
 * It is a plain .ts module (not .tsx) so the strings are declared once, in one reviewable place,
 * instead of scattered through JSX.
 */

import { fmtDateTime } from "@/lib/format";

export const NOTIFICATION_COPY_FALLBACKS: Record<string, string> = {
  "notifications.open": "Notifications",
  "notifications.title": "Notifications",
  "notifications.subtitle": "Newest first",
  "notifications.empty": "Nothing new. Task mentions and status changes addressed to you land here.",
  "notifications.unavailable": "Notifications are not switched on for this account yet.",
  "notifications.error": "Notifications could not be loaded. Try again in a moment.",
  "notifications.mark_all_read": "Mark all as read",
  "notifications.mark_read": "Mark as read",
  "notifications.refresh": "Refresh",
  "notifications.close": "Close notifications",
  "notifications.unread_one": "unread",
  "notifications.unread_more": "9+",
  "notifications.actor_prefix": "by",
  "notifications.open_target": "Open",
  "notifications.busy": "Working…",
  "mention.hint": "Type @ to mention someone",
  "mention.people_label": "People you can mention",
  "mention.no_matches": "Nobody on the leadership roster matches that.",
  "mention.mentioned_label": "Mentioned",
  "mention.remove_mention": "Remove mention",
};

export type NotificationCentreCopy = {
  open: string;
  title: string;
  subtitle: string;
  empty: string;
  unavailable: string;
  error: string;
  markAllRead: string;
  markRead: string;
  refresh: string;
  close: string;
  unreadOne: string;
  unreadMore: string;
  actorPrefix: string;
  openTarget: string;
  busy: string;
};

export type MentionComposerCopy = {
  hint: string;
  peopleLabel: string;
  noMatches: string;
  mentionedLabel: string;
  removeMention: string;
};

/** Backend copy wins; the fallback map answers only for a key the contract does not carry yet. */
export function notificationCopy(contractCopy: Record<string, string> | undefined, key: string): string {
  const served = contractCopy?.[key];
  if (typeof served === "string" && served.trim()) return served;
  return NOTIFICATION_COPY_FALLBACKS[key] ?? "";
}

export function resolveNotificationCentreCopy(
  contractCopy: Record<string, string> | undefined,
  openLabel?: string,
): NotificationCentreCopy {
  return {
    // The bell's own tooltip is backend-owned copy: `top_bar.notifications.label` while the
    // control is enabled, `top_bar.notifications.disabled_reason` only when it is not. The shell
    // resolves which one applies and passes it in.
    open: (openLabel ?? "").trim() || notificationCopy(contractCopy, "notifications.open"),
    title: notificationCopy(contractCopy, "notifications.title"),
    subtitle: notificationCopy(contractCopy, "notifications.subtitle"),
    empty: notificationCopy(contractCopy, "notifications.empty"),
    unavailable: notificationCopy(contractCopy, "notifications.unavailable"),
    error: notificationCopy(contractCopy, "notifications.error"),
    markAllRead: notificationCopy(contractCopy, "notifications.mark_all_read"),
    markRead: notificationCopy(contractCopy, "notifications.mark_read"),
    refresh: notificationCopy(contractCopy, "notifications.refresh"),
    close: notificationCopy(contractCopy, "notifications.close"),
    unreadOne: notificationCopy(contractCopy, "notifications.unread_one"),
    unreadMore: notificationCopy(contractCopy, "notifications.unread_more"),
    actorPrefix: notificationCopy(contractCopy, "notifications.actor_prefix"),
    openTarget: notificationCopy(contractCopy, "notifications.open_target"),
    busy: notificationCopy(contractCopy, "notifications.busy"),
  };
}

export function resolveMentionComposerCopy(contractCopy: Record<string, string> | undefined): MentionComposerCopy {
  return {
    hint: notificationCopy(contractCopy, "mention.hint"),
    peopleLabel: notificationCopy(contractCopy, "mention.people_label"),
    noMatches: notificationCopy(contractCopy, "mention.no_matches"),
    mentionedLabel: notificationCopy(contractCopy, "mention.mentioned_label"),
    removeMention: notificationCopy(contractCopy, "mention.remove_mention"),
  };
}

/**
 * The row's timestamp: `DD/MM/YYYY HH:MM` on the farm clock, the one shape every screen uses.
 *
 * Absolute, not "3 hours ago": the notification centre is read alongside a task list that shows
 * real deadlines, and two different time vocabularies on one screen is how a leader misreads one.
 *
 * IT CALLS THE SHARED HELPER AND HAS NO FORMAT OF ITS OWN. This used to hand-roll an
 * `Intl.DateTimeFormat` with `day: "2-digit", month: "short"` and the READER'S LOCALE, rendering
 * `18 Sep, 14:32` — a second date shape, one panel over from tables printing `18/09/2026`. The
 * 2026-09-10 maintainer lock is that every visible date is `DD/MM/YYYY` in full, with no compact
 * variant, because the whole point of that lock was three surfaces disagreeing about one fact. A
 * farm-readable date is also not a locale question, so the `locale` argument is gone with it.
 */
export function formatNotificationTime(iso: string): string {
  return fmtDateTime(iso);
}
