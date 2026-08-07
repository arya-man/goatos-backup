"use client";

import Link from "@/components/no-prefetch-link";
import { useCallback, useEffect, useRef, useState, type ComponentProps, type MouseEvent, type RefObject } from "react";

export const LOCAL_OVERLAY_URL_CHANGE_EVENT = "mesha:local-overlay-url-change";
const LOCAL_OVERLAY_HISTORY_KEY = "__meshaLocalOverlay";
// Backstop for the enter-transition frame; see showDrawer below.
const OPEN_FALLBACK_MS = 50;

export function notifyLocalOverlayUrlChange(): void {
  window.dispatchEvent(new Event(LOCAL_OVERLAY_URL_CHANGE_EVENT));
}

export function currentHistoryEntryIsLocalOverlay(): boolean {
  const state = window.history.state;
  return Boolean(state && typeof state === "object" && state[LOCAL_OVERLAY_HISTORY_KEY]);
}

export function replaceLocalOverlayUrl(href: string): void {
  const state = window.history.state && typeof window.history.state === "object" ? window.history.state : {};
  const { [LOCAL_OVERLAY_HISTORY_KEY]: _overlay, ...nextState } = state;
  void _overlay;
  window.history.replaceState(nextState, "", href);
  notifyLocalOverlayUrlChange();
}

type LocalOverlayLinkProps = ComponentProps<typeof Link>;

/**
 * Opens a same-page overlay from data that is already rendered on the client.
 * The href remains a real deep link for new tabs and no-JS fallback, while an
 * ordinary click updates browser history without requesting a new RSC payload.
 */
export function LocalOverlayLink({ onClick, ...props }: LocalOverlayLinkProps) {
  function openLocally(event: MouseEvent<HTMLAnchorElement>): void {
    onClick?.(event);
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;

    const nextUrl = new URL(event.currentTarget.href, window.location.href);
    if (nextUrl.origin !== window.location.origin || nextUrl.pathname !== window.location.pathname) return;

    event.preventDefault();
    if (nextUrl.href === window.location.href) {
      notifyLocalOverlayUrlChange();
      return;
    }
    const state = window.history.state && typeof window.history.state === "object" ? window.history.state : {};
    window.history.pushState({ ...state, [LOCAL_OVERLAY_HISTORY_KEY]: true }, "", nextUrl);
    notifyLocalOverlayUrlChange();
  }

  return <Link {...props} data-local-overlay-navigation="true" onClick={openLocally} />;
}

/**
 * Owns the lifecycle for a local drawer whose selected record is mirrored in
 * the current URL. This deliberately listens to history/hash changes instead
 * of Next's router so ordinary open/close clicks never request an RSC payload.
 */
export function useLocalOverlaySelection<T>({
  items,
  itemId,
  selectionKey,
  initialSelectedId,
  closeHref,
  transitionMs = 280,
}: {
  items: readonly T[];
  itemId: (item: T) => string;
  selectionKey: string;
  initialSelectedId?: string;
  closeHref: string;
  transitionMs?: number;
}): {
  displayedItem: T | undefined;
  drawerOpen: boolean;
  closeDrawer: () => void;
  closeButtonRef: RefObject<HTMLButtonElement | null>;
} {
  const initialItem = items.find((item) => itemId(item) === initialSelectedId);
  const [displayedItem, setDisplayedItem] = useState<T | undefined>(initialItem);
  const [drawerOpen, setDrawerOpen] = useState(Boolean(initialItem));
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const openFallbackRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  // The open class must never depend on one animation frame landing. That frame
  // can be cancelled by an effect re-run, or never fire at all when the tab is
  // backgrounded or the machine is loaded — which strands an already-mounted
  // drawer off-screen behind its scrim, so the user sees the shade and no panel.
  // A timer races the frame; whichever lands first opens the drawer.
  const cancelPendingOpen = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (openFallbackRef.current !== null) window.clearTimeout(openFallbackRef.current);
    openFrameRef.current = null;
    openFallbackRef.current = null;
  }, []);

  const showDrawer = useCallback(
    (item: T): void => {
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
      previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setDisplayedItem(item);
      cancelPendingOpen();
      const open = (): void => {
        cancelPendingOpen();
        setDrawerOpen(true);
      };
      openFrameRef.current = window.requestAnimationFrame(open);
      openFallbackRef.current = window.setTimeout(open, OPEN_FALLBACK_MS);
    },
    [cancelPendingOpen],
  );

  const hideDrawer = useCallback((): void => {
    cancelPendingOpen();
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    setDrawerOpen(false);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedItem(undefined);
      closeTimerRef.current = null;
    }, transitionMs);
  }, [cancelPendingOpen, transitionMs]);

  useEffect(() => {
    function syncSelectionFromUrl(): void {
      const url = new URL(window.location.href);
      const hashParams = new URLSearchParams(url.hash.replace(/^#/, ""));
      const selectedId = hashParams.get(selectionKey) ?? url.searchParams.get(selectionKey) ?? undefined;
      const item = items.find((candidate) => itemId(candidate) === selectedId);
      if (item) showDrawer(item);
      else hideDrawer();
    }
    window.addEventListener("popstate", syncSelectionFromUrl);
    window.addEventListener("hashchange", syncSelectionFromUrl);
    window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    // Sync immediately: a deep-linked drawer must not wait for a frame that a
    // hidden or busy tab may never deliver.
    syncSelectionFromUrl();
    return () => {
      window.removeEventListener("popstate", syncSelectionFromUrl);
      window.removeEventListener("hashchange", syncSelectionFromUrl);
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    };
  }, [hideDrawer, itemId, items, selectionKey, showDrawer]);

  // Pending open/close work is cancelled only on real unmount, never on an
  // effect re-run caused by a new items/itemId identity.
  useEffect(
    () => () => {
      cancelPendingOpen();
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    },
    [cancelPendingOpen],
  );

  useEffect(() => {
    if (drawerOpen) {
      const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
      return () => window.cancelAnimationFrame(frame);
    }
    previousFocusRef.current?.focus();
  }, [drawerOpen]);

  const closeDrawer = useCallback((): void => {
    hideDrawer();
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref, hideDrawer]);

  useEffect(() => {
    if (!drawerOpen) return undefined;
    function onKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeDrawer();
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [closeDrawer, drawerOpen]);

  return { displayedItem, drawerOpen, closeDrawer, closeButtonRef };
}
