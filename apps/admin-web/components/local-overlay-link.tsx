"use client";

import Link from "@/components/no-prefetch-link";
import { useCallback, useEffect, useRef, useState, type ComponentProps, type MouseEvent, type RefObject } from "react";

export const LOCAL_OVERLAY_URL_CHANGE_EVENT = "mesha:local-overlay-url-change";
const LOCAL_OVERLAY_HISTORY_KEY = "__meshaLocalOverlay";

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
  const closeTimerRef = useRef<number | null>(null);

  const showDrawer = useCallback((item: T): void => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setDisplayedItem(item);
    openFrameRef.current = window.requestAnimationFrame(() => {
      setDrawerOpen(true);
      openFrameRef.current = null;
    });
  }, []);

  const hideDrawer = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    setDrawerOpen(false);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedItem(undefined);
      closeTimerRef.current = null;
    }, transitionMs);
  }, [transitionMs]);

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
    const initialFrame = window.requestAnimationFrame(syncSelectionFromUrl);
    return () => {
      window.cancelAnimationFrame(initialFrame);
      window.removeEventListener("popstate", syncSelectionFromUrl);
      window.removeEventListener("hashchange", syncSelectionFromUrl);
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
      if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    };
  }, [hideDrawer, itemId, items, selectionKey, showDrawer]);

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
