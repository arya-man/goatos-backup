"use client";

/**
 * The top bar's notification bell: unread badge, and a popover listing the caller's recent
 * notifications.
 *
 * WHY IN-APP AT ALL, given a browser-push agent is also at work: push only fires when Chrome is
 * running AND the leader granted permission in that profile. The bell always works, so it is the
 * reliable channel and push is the accelerator. The push agent's permission control mounts into
 * the panel's ⚙ section (`./push-settings`) rather than as a second bell.
 *
 * REFRESH STRATEGY — ONE FEED READ PER SESSION, THEN A CHEAP BADGE POLL.
 *   1. the full page once on mount,
 *   2. the full page on open, so the list a reader is about to read is the freshest one,
 *   3. the full page on the explicit Refresh button,
 *   4. the BADGE ONLY (a limit=1 GET: `fetchNotificationBadge`) every 60 s while the tab is
 *      visible, and once more when a hidden tab becomes visible again after that long.
 * The full page is NOT re-read on route change any more. It used to be (the shell's `pathname`
 * was the trigger), and on a production build that was the single most frequent API call the
 * app made -- one 20-row feed read (~350 ms p50 on staging) for every sidebar click, on every
 * screen, whether or not anyone looked at the bell (Judge P, 2026-09-19: 100 feed reads in one
 * 56-route pass). The badge is what has to stay current between opens, and a one-row read
 * every minute in a visible tab is a bounded, predictable cost that a hidden tab never pays.
 *
 * FAILURE IS NEVER THE SHELL'S PROBLEM. This component wraps every call in try/catch and holds
 * its own error code: a missing endpoint, a 500, or a thrown action leaves the bell quiet and
 * every screen in the app rendering exactly as before. The badge simply does not appear.
 */

import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { m } from "motion/react";
import Badge from "@mui/material/Badge";
import IconButton from "@mui/material/IconButton";
import { Iconify } from "@/layouts/template/iconify";
import { varTap, varHover, transitionTap } from "@/layouts/template/animate";
import { MinimalDrawer } from "@/components/app/drawer";
import { NotificationSkeleton } from "./notification-skeleton";
import { markNotificationsReadAction, type NotificationFeedActionResult } from "./notification-actions";
import { useBackCloses } from "@/components/use-back-closes";
import { fetchNotificationBadge, fetchNotificationFeed } from "./notification-feed-client";
import { resolveNotificationCentreCopy } from "./notification-copy";
import {
  applyLocallyRead,
  EMPTY_NOTIFICATION_FEED,
  idsToMarkAllRead,
  NOTIFICATION_BADGE_MAX,
  notificationBadgeCount,
  type NotificationFeed,
} from "./notification-model";

/**
 * THE PANEL IS FETCHED ON THE FIRST OPEN, NOT WITH THE SHELL. The bell is mounted in the top bar
 * of every admin route, so everything it imports statically is paid for on all 63 of them -- and
 * the panel is the heavy half (its rows, its icons, its `no-prefetch-link`), for a popover most
 * readers never open. The BELL and its unread BADGE stay eager on purpose: a person must see that
 * they have notifications without interacting with anything.
 *
 * WHAT THIS DOES NOT CHANGE: nothing is measured. The panel lives in the template temporary drawer
 * (MinimalDrawer, right, the notifications-drawer 420 width), so the chunk lands inside an already
 * width-fixed paper and the first click still opens on the first try.
 */

/** Badge-only poll period while the tab is visible; see the refresh-strategy note above. */
const NOTIFICATION_BADGE_POLL_MS = 60_000;

const NotificationPanel = lazy(async () => {
  const mod = await import("./notification-panel");
  return { default: mod.NotificationPanel };
});
// While the panel chunk itself is in flight on the very first open, the surface carries an EAGER
// skeleton of the same shape (`./notification-skeleton`, kit Skeleton only, ~0.3 KB) so the box is
// never empty. Once the chunk lands the panel's own `loading` prop takes over.

export function NotificationBell({
  openLabel,
  contractCopy,
}: {
  /** Backend-owned label for the bell itself (`top_bar.notifications.*`), passed by the shell. */
  openLabel: string;
  /** The bootstrap contract's copy map. Backend keys win over the local fallbacks. */
  contractCopy?: Record<string, string>;
}) {
  const centreCopy = useMemo(() => resolveNotificationCentreCopy(contractCopy, openLabel), [contractCopy, openLabel]);
  const [feed, setFeed] = useState<NotificationFeed>(EMPTY_NOTIFICATION_FEED);
  const [locallyRead, setLocallyRead] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  // True until the first feed read has answered (ok or not): the panel shows skeleton rows for
  // that window instead of an empty state that would flash into a list a beat later.
  const [loaded, setLoaded] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  // A refresh in flight with nothing loaded yet (first open before the mount read answered, or a
  // read that failed and is being retried) shows skeleton rows rather than an empty list.
  const [refreshing, setRefreshing] = useState(false);
  const [errorCode, setErrorCode] = useState<string | undefined>(undefined);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
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
    setLoaded(true);
    if (!result.ok) {
      setErrorCode(result.code);
      return;
    }
    setErrorCode(undefined);
    setFeed(result.feed);
  }, []);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      applyFeedResult(await fetchNotificationFeed());
    } catch {
      // A thrown Server Action (network drop, a deploy mid-flight) must not take the shell with
      // it: the bell simply stays quiet and every screen renders exactly as before.
      if (liveRef.current) setLoaded(true);
    } finally {
      if (liveRef.current) setRefreshing(false);
    }
  }, [applyFeedResult]);

  // The next keyset page, APPENDED (deduped by id) under the rows already shown; the server's
  // unread total and the new cursor replace the old ones. A refresh still resets to page one.
  const loadMore = useCallback(async () => {
    const cursor = feed.next_cursor;
    if (!cursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const result = await fetchNotificationFeed(cursor);
      if (!liveRef.current) return;
      if (!result.ok) {
        setErrorCode(result.code);
        return;
      }
      setFeed((current) => {
        const seen = new Set(current.items.map((item) => item.notification_request_id));
        return {
          ...current,
          items: [...current.items, ...result.feed.items.filter((item) => !seen.has(item.notification_request_id))],
          unread_count: result.feed.unread_count,
          total_count: result.feed.total_count ?? current.total_count,
          push_available: result.feed.push_available ?? current.push_available,
          next_cursor: result.feed.next_cursor,
        };
      });
    } catch {
      // Same silence as a failed refresh: the rows already shown stay, the reader can retry.
    } finally {
      if (liveRef.current) setLoadingMore(false);
    }
  }, [feed.next_cursor, loadingMore]);

  // Mount ONLY. See the refresh-strategy note above: the route change trigger is gone; the badge
  // poll below keeps the count current and the open handler re-reads the page.
  useEffect(() => {
    let cancelled = false;
    // The first read must NOT be dispatched from inside the hydration commit. A Server Action goes
    // through the App Router's action queue, and on a fresh page load that queue is not
    // initialised yet -- Next throws `Internal Next.js error: Router action dispatched before
    // initialization` from its own internals, where this component's try/catch cannot reach it,
    // and the read is lost until the next route change. One macrotask is enough, and the bell has
    // no deadline: nobody is reading an unread badge in the first frame.
    const timer = setTimeout(() => {
      void (async () => {
        try {
          const result = await fetchNotificationFeed();
          if (!cancelled) applyFeedResult(result);
        } catch {
          // Same silence as above; a failed read is not the shell's problem.
          if (!cancelled && liveRef.current) setLoaded(true);
        }
      })();
    }, 0);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [applyFeedResult]);

  // BADGE POLL: unread total only, every 60 s, only while the document is visible, and never
  // while the panel is open (the open handler already read the freshest page, and a badge that
  // moved under an open list would disagree with the rows on screen). A tab that comes back from
  // the background re-reads at once if its last read is older than the interval, so a laptop
  // opened in the morning shows the night's count without waiting a minute. `openRef` mirrors
  // `open` so the interval never has to be re-armed on every toggle.
  const openRef = useRef(open);
  useEffect(() => {
    openRef.current = open;
  }, [open]);
  useEffect(() => {
    let cancelled = false;
    let lastReadAt = Date.now();
    const poll = async () => {
      if (cancelled || openRef.current || document.visibilityState !== "visible") return;
      lastReadAt = Date.now();
      try {
        const result = await fetchNotificationBadge();
        if (cancelled || !liveRef.current || !result.ok || openRef.current) return;
        setFeed((current) => (current.unread_count === result.unreadCount ? current : { ...current, unread_count: result.unreadCount }));
      } catch {
        // A failed poll is silent: the badge keeps the last count it had.
      }
    };
    const interval = setInterval(() => void poll(), NOTIFICATION_BADGE_POLL_MS);
    const onVisible = () => {
      if (document.visibilityState === "visible" && Date.now() - lastReadAt >= NOTIFICATION_BADGE_POLL_MS) void poll();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, []);

  const closePanel = useCallback(() => {
    setOpen(false);
  }, []);
  // Back closes the drawer (house drawer rule: X, Escape, scrim and Back).
  useBackCloses(open, closePanel);

  // The popover closes with the rest of the top bar's menus: a click outside any menu root, or
  // Escape. `data-menu-root` on the wrapper is the shell's own convention.

  // The sheet owns its own closing (scrim click, Escape, focus trap) and its own geometry: full
  // viewport height from the right, 420px or the whole width on a phone. Nothing is measured.

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
      // `flex: none` keeps the bell the same size it was as a disabled button, so no other
      // top-bar control moves.
      style={{ position: "relative", flex: "none" }}
    >
      {/* Template notifications-drawer trigger (layouts/components/notifications-drawer): IconButton
          with the tap/hover motion, MUI Badge (error) and the 24px solar bell. */}
      <IconButton
        ref={buttonRef}
        component={m.button}
        whileTap={varTap(0.96)}
        whileHover={varHover(1.04)}
        transition={transitionTap()}
        title={centreCopy.open}
        aria-label={centreCopy.open}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => {
          // No updater on purpose: a state updater must be pure (React runs it twice in
          // StrictMode). `open` is already in this closure -- `aria-expanded` above reads it -- so
          // the toggle needs none, and the refresh runs out here exactly once per click.
          if (open) {
            closePanel();
            return;
          }
          setOpen(true);
          void refresh();
        }}
      >
        <Badge badgeContent={badge > 0 ? badgeText : null} color="error">
          <Iconify width={24} icon="solar:bell-bing-bold-duotone" />
        </Badge>
      </IconButton>
      {/* The template temporary drawer (MinimalDrawer) at the template notifications-drawer width
          (layouts/components/notifications-drawer: 420 max, the whole width on a phone): pinned
          header with the title and close, visible backdrop (Ravi R2-4), Scrollbar body with the
          panel's actions, tabs and list. MUI portals it to <body> (`.top`'s backdrop-filter would
          otherwise pin a fixed sheet inside the bar) and owns Escape, focus trap and body scroll
          lock. */}
      <MinimalDrawer
        open={open}
        onClose={closePanel}
        title={centreCopy.title}
        closeLabel={centreCopy.close}
        width={420}
        slotProps={{ paper: { className: "nc-sheet", role: "dialog", "aria-label": centreCopy.title } as object }}
      >
              <Suspense fallback={<NotificationSkeleton />}>
          <NotificationPanel
            feed={shownFeed}
            centreCopy={centreCopy}
            busy={busy}
            loading={!loaded || (refreshing && shownFeed.items.length === 0)}
            errorCode={errorCode}
            contractCopy={contractCopy}
            pushAvailable={feed.push_available === true}
            onMarkRead={(id) => void markRead([id])}
            onMarkAllRead={() => void markRead(idsToMarkAllRead(feed, locallyRead))}
            onRefresh={() => void refresh()}
            onClose={closePanel}
            onLoadMore={() => void loadMore()}
            loadingMore={loadingMore}
          />
              </Suspense>
      </MinimalDrawer>
    </div>
  );
}
