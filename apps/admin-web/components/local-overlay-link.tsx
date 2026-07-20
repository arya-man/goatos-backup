"use client";

import Link from "@/components/no-prefetch-link";
import type { ComponentProps, MouseEvent } from "react";

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
