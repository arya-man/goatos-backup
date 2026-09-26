"use client";

import { useEffect } from "react";

/**
 * Settles the address bar on a canonical href WITHOUT a second document load: the page has
 * already rendered with the defaults the href names (e.g. the weighing window), so the URL is
 * corrected in place with `history.replaceState`. Next's patched replaceState keeps its router
 * state (usePathname/useSearchParams) in sync. Nothing happens when the URL already matches.
 */
export function CanonicalUrl({ href }: { href: string }) {
  useEffect(() => {
    const next = new URL(href, window.location.href);
    const current = new URL(window.location.href);
    if (next.pathname === current.pathname && next.search === current.search) return;
    window.history.replaceState(window.history.state, "", next.toString());
  }, [href]);
  return null;
}
