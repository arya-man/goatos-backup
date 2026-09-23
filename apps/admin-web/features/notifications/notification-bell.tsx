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
import { Suspense, lazy, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
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
  sortNotificationsNewestFirst,
  type NotificationFeed,
} from "./notification-model";
import { placeNotificationPanel, type NotificationPanelBox } from "./notification-placement";

/**
 * THE PANEL IS FETCHED ON THE FIRST OPEN, NOT WITH THE SHELL. The bell is mounted in the top bar
 * of every admin route, so everything it imports statically is paid for on all 63 of them -- and
 * the panel is the heavy half (its rows, its icons, its `no-prefetch-link`), for a popover most
 * readers never open. The BELL and its unread BADGE stay eager on purpose: a person must see that
 * they have notifications without interacting with anything.
 *
 * WHAT THIS DOES NOT CHANGE, and it is the thing to check before editing anything below: the
 * measured geometry. `panelRef` is on the `.parkmenu` WRAPPER, which is rendered on every pass
 * whether the panel's chunk has arrived or not, and `placeNotificationPanel` derives the box from
 * the BELL's rect and the viewport width -- never from the panel's content. So the layout effect
 * measures the same element at the same time it always did, the chunk lands inside an already
 * placed and already width-fixed box, and the first click still opens on the first try.
 */
const NotificationPanel = lazy(async () => {
  const mod = await import("./notification-panel");
  return { default: mod.NotificationPanel };
});

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
  const [loadingMore, setLoadingMore] = useState(false);
  const [errorCode, setErrorCode] = useState<string | undefined>(undefined);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const liveRef = useRef(true);
  // WHERE THE PANEL SITS. The arithmetic and the reason it exists at all are in
  // `./notification-placement`. What lives here is the MEASUREMENT, and the rule that goes with
  // it: this is never a cached value. It is null whenever the panel is closed and is measured
  // afresh, before paint, every single time the panel opens (see the layout effect below). A
  // `panelBox` that survived a close would be coordinates for a viewport that may no longer
  // exist -- WhatsApp's chrome retracting and a phone rotating both change the width while
  // nobody has the panel open, and a value measured at 1440px (left ~1132) would put the panel
  // far off the right edge of a 360px phone.
  const [panelBox, setPanelBox] = useState<NotificationPanelBox | null>(null);

  useEffect(() => {
    liveRef.current = true;
    return () => {
      liveRef.current = false;
    };
  }, []);

  // Applying a loaded page is its own step so that every caller -- the effect below, the open
  // handler, the Refresh button -- reaches state only AFTER the await, never synchronously inside
  // an effect body (react-hooks/set-state-in-effect).
  const appendFeedPage = useCallback((current: NotificationFeed, page: NotificationFeed): NotificationFeed => {
    const seen = new Set(current.items.map((item) => item.notification_request_id));
    return {
      ...page,
      items: sortNotificationsNewestFirst([
        ...current.items,
        ...page.items.filter((item) => !seen.has(item.notification_request_id)),
      ]),
      unread_count: Math.max(current.unread_count, page.unread_count),
      available: current.available || page.available,
    };
  }, []);

  const applyFeedResult = useCallback((result: NotificationFeedActionResult, mode: "replace" | "append" = "replace") => {
    if (!liveRef.current) return;
    if (!result.ok) {
      setErrorCode(result.code);
      return;
    }
    setErrorCode(undefined);
    setFeed((current) => (mode === "append" ? appendFeedPage(current, result.feed) : result.feed));
  }, [appendFeedPage]);

  const refresh = useCallback(async () => {
    try {
      applyFeedResult(await loadNotificationFeedAction());
    } catch {
      // A thrown Server Action (network drop, a deploy mid-flight) must not take the shell with
      // it: the bell simply stays quiet and every screen renders exactly as before.
    }
  }, [applyFeedResult]);

  const loadMore = useCallback(async () => {
    const cursor = feed.next_cursor;
    if (!cursor || loadingMore || busy) return;
    setLoadingMore(true);
    try {
      applyFeedResult(await loadNotificationFeedAction(cursor), "append");
    } catch {
      // Same silence as refresh: a missed older page must not destabilise the shell.
    } finally {
      if (liveRef.current) setLoadingMore(false);
    }
  }, [applyFeedResult, busy, feed.next_cursor, loadingMore]);

  // Mount + every route change. See the refresh-strategy note above: a leader's session is a walk
  // through screens, so navigation is the revalidation point and there is no interval anywhere.
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
          const result = await loadNotificationFeedAction();
          if (!cancelled) applyFeedResult(result);
        } catch {
          // Same silence as above; a failed read is not the shell's problem.
        }
      })();
    }, 0);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [applyFeedResult, pathname]);

  // Closing DROPS the measurement, so a stale box can never be painted on the next open. The
  // layout effect below is the only thing that ever sets one.
  const closePanel = useCallback(() => {
    setOpen(false);
    setPanelBox(null);
  }, []);

  // The popover closes with the rest of the top bar's menus: a click outside any menu root, or
  // Escape. `data-menu-root` on the wrapper is the shell's own convention.
  useEffect(() => {
    if (!open) return;
    function onDown(event: MouseEvent) {
      const element = event.target as HTMLElement | null;
      if (element && rootRef.current?.contains(element)) return;
      closePanel();
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") closePanel();
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [closePanel, open]);

  const placePanel = useCallback(() => {
    const panel = panelRef.current;
    const anchor = buttonRef.current?.getBoundingClientRect();
    if (!panel || !anchor) return;
    const target = placeNotificationPanel(anchor, document.documentElement.clientWidth);
    // CONTAINING-BLOCK CORRECTION, HORIZONTAL ONLY. `.top` carries `backdrop-filter`, which makes
    // it the containing block for this `position:fixed` panel -- so `left`/`top` resolve against
    // `.top`'s border box, not the viewport, while `target` is in viewport space. Today `.top` sits
    // at (0,0) at full width so the two frames coincide, which is load-bearing coincidence rather
    // than design: a margin, an offset or a transform on `.top` would silently shift the panel by
    // that much, and horizontally that is the difference between on screen and off it. So rather
    // than depend on the shell's geometry, ask for the coordinates and then correct by however far
    // the element actually landed from where we asked. Both reads happen inside a layout effect (or
    // a synchronous event handler), so no uncorrected frame is ever painted.
    //
    // ONLY X IS CORRECTED, deliberately. `.parkmenu` animates itself in with
    // `transform:translateY(-6px)` -> `transform:none` over 160ms, so the panel's own VERTICAL
    // offset is mid-transition at the instant this measures; correcting against it would fight the
    // animation and leave the panel 6px low once the transition settled. A vertical offset on
    // `.top` would move the panel down with the bar it hangs from and it stays reachable, so the
    // trade is worth taking. Horizontal has no transform and no such excuse.
    panel.style.position = "fixed";
    panel.style.right = "auto";
    panel.style.width = `${target.width}px`;
    panel.style.left = `${target.left}px`;
    panel.style.top = `${target.top}px`;
    const landed = panel.getBoundingClientRect();
    setPanelBox({
      top: target.top,
      left: Math.round(target.left + (target.left - landed.left)),
      width: target.width,
    });
  }, []);

  // MEASURE AFTER THE PANEL IS VISIBLE, BEFORE IT IS PAINTED -- which is why this is a LAYOUT
  // effect keyed on `open`, and not the click handler it used to be.
  //   * Not inside the `setOpen` updater, which is where it was: a state updater must be a pure
  //     function of the previous state, because React invokes it twice in StrictMode and may
  //     re-run it when rebasing an update. Measuring and dispatching from in there produced a real
  //     `Cannot update a component (Router) while rendering a different component
  //     (NotificationBell)` on every open.
  //   * Not the click handler either, because the panel's own box is one of the two things being
  //     measured (the containing-block correction above), and on the first open the panel is still
  //     `display:none` via `.parkmenu:not(.on)` when the handler runs -- a zero-size element
  //     measures as zero.
  //   * A layout effect runs after React has committed `open` to the DOM, so `.parkmenu.on` is
  //     laid out and measurable, and before the browser paints, so there is no frame in which the
  //     panel is on screen at the wrong coordinates.
  useLayoutEffect(() => {
    if (!open) return;
    placePanel();
  }, [open, placePanel]);

  // Re-place while the panel is open: the viewport can change under it (rotation, WhatsApp's
  // retracting chrome) and the top bar is not sticky on every route, so a scroll can move the
  // bell. These listeners only need to exist while open -- every OPEN re-measures from scratch, so
  // a resize that happens while the panel is closed needs nothing kept up to date.
  useEffect(() => {
    if (!open) return;
    const reposition = () => placePanel();
    window.addEventListener("resize", reposition);
    window.addEventListener("orientationchange", reposition);
    window.addEventListener("scroll", reposition, true);
    return () => {
      window.removeEventListener("resize", reposition);
      window.removeEventListener("orientationchange", reposition);
      window.removeEventListener("scroll", reposition, true);
    };
  }, [open, placePanel]);

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
        ref={buttonRef}
        type="button"
        className="iconbtn"
        title={centreCopy.open}
        aria-label={centreCopy.open}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => {
          // The updater is gone on purpose: it used to call `placePanel()` and `void refresh()`
          // from inside `setOpen`, and a state updater must be pure. `open` is already in this
          // closure -- `aria-expanded` above reads it -- so the toggle needs no updater, and the
          // two effects belong out here where they run exactly once per click. Placement itself is
          // the layout effect's job now, not this handler's.
          if (open) {
            closePanel();
            return;
          }
          setOpen(true);
          void refresh();
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
      <div
        ref={panelRef}
        className={`parkmenu ${open ? "on" : ""}`}
        role="dialog"
        aria-label={centreCopy.title}
        aria-modal={false}
        // `fixed`, not the sheet's `absolute`: the bell's own box is what put the panel off-screen,
        // so the panel is taken out of it. Inline because the coordinates are measured, and these
        // four properties are exactly the ones `.parkmenu` (and its <=760px override) would
        // otherwise win with.
        style={
          open && panelBox
            ? {
                position: "fixed",
                top: panelBox.top,
                left: panelBox.left,
                right: "auto",
                width: panelBox.width,
                maxWidth: panelBox.width,
              }
            : undefined
        }
      >
        {open ? (
          // `fallback={null}` keeps the popover EMPTY for the tick the chunk takes rather than
          // showing a spinner that would resize the box the layout effect has just measured.
          <Suspense fallback={null}>
          <NotificationPanel
            feed={shownFeed}
            centreCopy={centreCopy}
            busy={busy}
            loadingMore={loadingMore}
            errorCode={errorCode}
            permissionSlot={permissionSlot}
            onMarkRead={(id) => void markRead([id])}
            onMarkAllRead={() => void markRead(idsToMarkAllRead(feed, locallyRead))}
            onRefresh={() => void refresh()}
            onLoadMore={() => void loadMore()}
            onClose={closePanel}
          />
          </Suspense>
        ) : null}
      </div>
    </div>
  );
}
