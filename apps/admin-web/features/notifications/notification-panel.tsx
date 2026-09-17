"use client";

/**
 * The notification centre's list, newest first: what happened, who did it, when, and a link to
 * the thing it refers to.
 *
 * Presentational on purpose — it owns no fetching and no optimism — so the bell drives it and a
 * fixture (or a future /notifications page) can render it unchanged.
 *
 * STYLING. Existing shared classes only: the `.parkmenu` popover idiom (the caller owns the
 * popover box), `.pm-item` rows, `.pm-hint` footer/notes, `.btn.sm`, `.muted`, `.pn`.
 * `app/mesha-theme.css` belongs to another agent this round and a second stylesheet is not
 * allowed, so the handful of rules no existing class expresses — the scroll cap, the phone tap
 * floor, the header divider — are INLINE and commented where they appear. Every one is listed in
 * the handover report.
 */

import Link from "@/components/no-prefetch-link";
import { Check, CheckCheck, RefreshCw, X } from "lucide-react";
import { formatNotificationTime, type NotificationCentreCopy } from "./notification-copy";
import {
  isNotificationRead,
  notificationHref,
  sortNotificationsNewestFirst,
  type InAppNotification,
  type NotificationFeed,
} from "./notification-model";

export function NotificationPanel({
  feed,
  centreCopy,
  busy = false,
  errorCode,
  permissionSlot,
  onMarkRead,
  onMarkAllRead,
  onRefresh,
  onClose,
  onNavigate,
}: {
  feed: NotificationFeed;
  centreCopy: NotificationCentreCopy;
  busy?: boolean;
  /** A backend error code. The sentence beside it is contract copy; nothing is composed here. */
  errorCode?: string;
  /**
   * Where the browser-push agent's "Enable notifications" control mounts: directly under the
   * header and above the first row, so a leader who has not granted Chrome permission sees the
   * offer the moment they open the bell. Passing nothing leaves no trace of it in the DOM.
   */
  permissionSlot?: React.ReactNode;
  onMarkRead: (notificationRequestId: string) => void;
  onMarkAllRead: () => void;
  onRefresh: () => void;
  onClose: () => void;
  onNavigate?: (item: InAppNotification) => void;
}) {
  const rows = sortNotificationsNewestFirst(feed.items);
  const hasUnread = rows.some((item) => !isNotificationRead(item));

  return (
    <div>
      {/* Header. Inline flex because no shared class carries a popover header row; `.pm-item`
          would have added a hover highlight to a non-interactive strip. */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          padding: "4px 10px 8px",
          borderBottom: "1px solid var(--line2)",
        }}
      >
        <b style={{ fontSize: 13, flex: 1, minWidth: 0 }}>{centreCopy.title}</b>
        <button
          type="button"
          className="btn sm ghost"
          style={{ minWidth: 40, minHeight: 40 }}
          onClick={onClose}
          title={centreCopy.close}
          aria-label={centreCopy.close}
        >
          <X className="ic" aria-hidden="true" />
        </button>
      </div>

      {permissionSlot ? <div style={{ padding: "8px 10px 0" }}>{permissionSlot}</div> : null}

      {errorCode ? (
        <div className="pm-hint" role="alert">
          <b>{errorCode}</b>
          &nbsp;
          <span>{centreCopy.error}</span>
        </div>
      ) : null}

      {/* The scroll box. `dvh`, never `vh`: inside the WhatsApp in-app webview the browser chrome
          retracts, so `vh` measures a viewport that is not on screen and the last row ends up
          under the on-screen keyboard. */}
      <ul
        style={{ maxHeight: "55dvh", overflowY: "auto", listStyle: "none", margin: 0, padding: 0 }}
        aria-busy={busy}
      >
        {rows.map((item) => {
          const href = notificationHref(item);
          const read = isNotificationRead(item);
          const when = formatNotificationTime(item.requested_at);
          return (
            <li key={item.notification_request_id}>
              {/* 44px floor so the row is a real tap target on a 390px phone. */}
              <div className="pm-item" style={{ minHeight: 44, alignItems: "flex-start" }}>
                <span style={{ flex: 1, minWidth: 0 }}>
                  {href ? (
                    <Link
                      href={href}
                      className="pn"
                      style={{ display: "block", whiteSpace: "normal", fontWeight: read ? 500 : 700 }}
                      onClick={() => {
                        onMarkRead(item.notification_request_id);
                        onNavigate?.(item);
                        onClose();
                      }}
                    >
                      {item.title}
                    </Link>
                  ) : (
                    <span className="pn" style={{ display: "block", whiteSpace: "normal", fontWeight: read ? 500 : 700 }}>
                      {item.title}
                    </span>
                  )}
                  {item.body ? (
                    <span className="muted" style={{ display: "block", fontSize: 12 }}>
                      {item.body}
                    </span>
                  ) : null}
                  <span className="muted" style={{ display: "block", fontSize: 11 }}>
                    {item.actor_name ? `${centreCopy.actorPrefix} ${item.actor_name} · ${when}` : when}
                  </span>
                </span>
                {read ? null : (
                  <button
                    type="button"
                    className="btn sm ghost"
                    style={{ minWidth: 40, minHeight: 40 }}
                    onClick={() => onMarkRead(item.notification_request_id)}
                    disabled={busy}
                    title={centreCopy.markRead}
                    aria-label={centreCopy.markRead}
                  >
                    <Check className="ic" aria-hidden="true" />
                  </button>
                )}
              </div>
            </li>
          );
        })}
      </ul>

      {rows.length === 0 ? (
        <div className="pm-hint">{feed.available ? centreCopy.empty : centreCopy.unavailable}</div>
      ) : null}

      <div className="pm-hint" style={{ display: "flex", gap: 8, alignItems: "center" }}>
        {hasUnread ? (
          <button type="button" className="btn sm" onClick={onMarkAllRead} disabled={busy}>
            <CheckCheck className="ic" aria-hidden="true" />
            {busy ? centreCopy.busy : centreCopy.markAllRead}
          </button>
        ) : null}
        <button
          type="button"
          className="btn sm ghost"
          style={{ marginLeft: "auto", minHeight: 40 }}
          onClick={onRefresh}
          disabled={busy}
          title={centreCopy.refresh}
          aria-label={centreCopy.refresh}
        >
          <RefreshCw className="ic" aria-hidden="true" />
        </button>
      </div>
    </div>
  );
}
