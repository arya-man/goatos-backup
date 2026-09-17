"use client";

/**
 * The top bar's notification bell: unread badge, and a popover listing the caller's recent
 * notifications.
 *
 * WHY IN-APP AT ALL, given a browser-push agent is also at work: push only fires when Chrome is
 * running AND the leader granted permission in that profile. The bell always works, so it is the
 * reliable channel and push is the accelerator. The push agent's permission control mounts into
 * `permissionSlot` below rather than as a second bell.
 *
 * REFRESH STRATEGY — NO POLLING LOOP.
 *   1. once on mount,
 *   2. on every route change (the shell's `pathname` is the trigger; a leader's session is a walk
 *      through screens, so navigation is the natural revalidation point and costs one read per
 *      screen a person actually visited),
 *   3. on open, so the list a reader is about to read is the freshest one,
 *   4. on the explicit Refresh button.
 * An interval would multiply by every open tab on every CEO's laptop all day for a feed that
 * changes a few times an hour; navigation-triggered reads cost nothing when nobody is looking.
 *
 * FAILURE IS NEVER THE SHELL'S PROBLEM. This component wraps every call in try/catch and holds
 * its own error code: a missing endpoint, a 500, or a thrown action leaves the bell quiet and
 * every screen in the app rendering exactly as before. The badge simply does not appear.
 */

import { usePathname } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Bell } from "lucide-react";
import {
  loadNotificationFeedAction,
  markNotificationsReadAction,
  type NotificationFeedActionResult,
} from "./notification-actions";
import { resolveNotificationCentreCopy } from "./notification-copy";
import {
  applyLocallyRead,
  EMPTY_NOTIFICATION_FEED,
  idsToMarkAllRead,
  NOTIFICATION_BADGE_MAX,
  notificationBadgeCount,
  type NotificationFeed,
} from "./notification-model";
import { NotificationPanel } from "./notification-panel";

export function NotificationBell({
  openLabel,
  contractCopy,
  permissionSlot,
}: {
  /** Backend-owned label for the bell itself (`top_bar.notifications.*`), passed by the shell. */
  openLabel: string;
  /** The bootstrap contract's copy map. Backend keys win over the local fallbacks. */
  contractCopy?: Record<string, string>;
  permissionSlot?: React.ReactNode;
}) {
  const pathname = usePathname() ?? "/";
  const centreCopy = useMemo(() => resolveNotificationCentreCopy(contractCopy, openLabel), [contractCopy, openLabel]);
  const [feed, setFeed] = useState<NotificationFeed>(EMPTY_NOTIFICATION_FEED);
  const [locallyRead, setLocallyRead] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [errorCode, setErrorCode] = useState<string | undefined>(undefined);
  const rootRef = useRef<HTMLDivElement>(null);
  const liveRef = useRef(true);

  useEffect(() => {
    liveRef.current = true;
    return () => {
      liveRef.current = false;
    };
  }, []);

  // Applying a loaded page is its own step so that every caller -- the effect below, the open
  // handler, the Refresh button -- reaches state only AFTER the await, never synchronously inside
  // an effect body (react-hooks/set-state-in-effect).
  const applyFeedResult = useCallback((result: NotificationFeedActionResult) => {
    if (!liveRef.current) return;
    if (!result.ok) {
      setErrorCode(result.code);
      return;
    }
    setErrorCode(undefined);
    setFeed(result.feed);
  }, []);

  const refresh = useCallback(async () => {
    try {
      applyFeedResult(await loadNotificationFeedAction());
    } catch {
      // A thrown Server Action (network drop, a deploy mid-flight) must not take the shell with
      // it: the bell simply stays quiet and every screen renders exactly as before.
    }
  }, [applyFeedResult]);

  // Mount + every route change. See the refresh-strategy note above: a leader's session is a walk
  // through screens, so navigation is the revalidation point and there is no interval anywhere.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const result = await loadNotificationFeedAction();
        if (!cancelled) applyFeedResult(result);
      } catch {
        // Same silence as above; a failed read is not the shell's problem.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [applyFeedResult, pathname]);

  // The popover closes with the rest of the top bar's menus: a click outside any menu root, or
  // Escape. `data-menu-root` on the wrapper is the shell's own convention.
  useEffect(() => {
    if (!open) return;
    function onDown(event: MouseEvent) {
      const element = event.target as HTMLElement | null;
      if (element && rootRef.current?.contains(element)) return;
      setOpen(false);
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const markRead = useCallback(async (ids: string[]) => {
    if (ids.length === 0) return;
    // Optimistic: the row goes quiet now, the write answers later. `applyLocallyRead` is the only
    // place that optimism lives, so a refresh cannot make a cleared row flash back to bold.
    setLocallyRead((current) => [...new Set([...current, ...ids])]);
    setBusy(true);
    try {
      const result = await markNotificationsReadAction(ids);
      if (!liveRef.current) return;
      if (!result.ok) setErrorCode(result.code);
      else setErrorCode(undefined);
    } catch {
      // Left marked read locally: the reader has seen it, and a failed write is retried by the
      // next mark-all rather than by un-reading a row under their eyes.
    } finally {
      if (liveRef.current) setBusy(false);
    }
  }, []);

  const shownFeed = useMemo(() => applyLocallyRead(feed, locallyRead), [feed, locallyRead]);
  const badge = notificationBadgeCount(feed, locallyRead);
  const badgeText = badge > NOTIFICATION_BADGE_MAX ? centreCopy.unreadMore : String(badge);

  return (
    <div
      ref={rootRef}
      data-menu-root
      data-notification-bell
      // `position: relative` is the anchor the shared `.parkmenu` popover positions against
      // (top: 46px; right: 0), exactly as `.parksel` and `.userpick` do for their own menus.
      // `flex: none` keeps the bell the same size it was as a disabled button, so no other
      // top-bar control moves.
      style={{ position: "relative", flex: "none" }}
    >
      <button
        type="button"
        className="iconbtn"
        title={centreCopy.open}
        aria-label={centreCopy.open}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => {
          setOpen((current) => {
            const next = !current;
            if (next) void refresh();
            return next;
          });
        }}
      >
        <Bell className="ic" />
        {badge > 0 ? (
          // The unread badge. `.badge-def` is the existing brand pill; only its placement over the
          // bell corner is inline, because no shared class positions a badge on an icon button.
          <span
            className="badge-def"
            style={{ position: "absolute", top: 2, right: 0, margin: 0, pointerEvents: "none" }}
          >
            {badgeText}
          </span>
        ) : null}
      </button>
      <div className={`parkmenu ${open ? "on" : ""}`} role="dialog" aria-label={centreCopy.title} aria-modal={false}>
        {open ? (
          <NotificationPanel
            feed={shownFeed}
            centreCopy={centreCopy}
            busy={busy}
            errorCode={errorCode}
            permissionSlot={permissionSlot}
            onMarkRead={(id) => void markRead([id])}
            onMarkAllRead={() => void markRead(idsToMarkAllRead(feed, locallyRead))}
            onRefresh={() => void refresh()}
            onClose={() => setOpen(false)}
          />
        ) : null}
      </div>
    </div>
  );
}
