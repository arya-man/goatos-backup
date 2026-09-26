"use client";

// telemetry:exempt presentational pending-navigation feedback — no user action, no data read

import { useEffect, useRef, useState } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { URL_NAV_EVENT, isCurrentUrl, type UrlNavDetail } from "@/components/app/url-tab-nav";

/** A navigation that never lands (a redirect elsewhere, a failed fetch) stops looking pending after this. */
const LINK_NAV_GIVE_UP_MS = 8000;

function navKeyOf(pathname: string | null, search: string): string {
  return `${pathname ?? "/"}?${search}`;
}

/**
 * Page-level feedback for every URL-driven strip (kit `AnimatedTabs` with hrefs, `SegmentedLinks`).
 *
 * Those strips navigate in a transition (`useUrlTabNav`): the router keeps the current page on
 * screen until the new one is ready. The strip moves its indicator at once and shows its progress
 * line; this component, mounted once in the shell beside the page, turns the strip's
 * `metricseg:navigate` event into `data-nav-pending` on the page column, which dims the body below
 * the header and the navigating strip and blocks its pointer (app/frame.css). The header, crumbs,
 * strip and filters never unmount and the page is never swapped for a skeleton. It clears the
 * moment the router lands on ANY new URL (a redirect counts as landing), and gives up after 8s so a
 * failed navigation never leaves the page dimmed.
 *
 * It renders no copy of its own.
 */
export function LinkNavPending() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const navKey = navKeyOf(pathname, searchParams?.toString() ?? "");
  // The URL the click left from. Pending only while the URL is still that one: the moment the
  // router lands anywhere (the target, or a redirect) the flag is inert without any state to clear.
  const [pendingFrom, setPendingFrom] = useState<string | null>(null);
  const root = useRef<HTMLDivElement | null>(null);
  const active = pendingFrom !== null && pendingFrom === navKey;

  useEffect(() => {
    const onNavigate = (event: Event) => {
      const href = (event as CustomEvent<Partial<UrlNavDetail>>).detail?.href;
      if (!href || isCurrentUrl(href, window.location)) return;
      // Only a same-route param change: a navigation to another route is that route's loading.tsx.
      try {
        if (new URL(href, window.location.href).pathname !== window.location.pathname) return;
      } catch {
        return;
      }
      setPendingFrom(navKeyOf(window.location.pathname, new URLSearchParams(window.location.search).toString()));
    };
    window.addEventListener(URL_NAV_EVENT, onNavigate);
    return () => window.removeEventListener(URL_NAV_EVENT, onNavigate);
  }, []);

  useEffect(() => {
    if (!active) return undefined;
    const from = pendingFrom;
    const giveUp = window.setTimeout(() => setPendingFrom((prev) => (prev === from ? null : prev)), LINK_NAV_GIVE_UP_MS);
    return () => window.clearTimeout(giveUp);
  }, [active, pendingFrom]);

  // The flag lives on the page column (this component's parent), so the CSS can reach the page
  // root and its strips as siblings without the page knowing this component exists.
  useEffect(() => {
    const column = root.current?.parentElement;
    if (!column) return undefined;
    // A page built on URL-keyed panels (UrlSuspense) shows each affected panel's skeleton instead;
    // dimming the whole body on top of that is the flicker this replaces.
    if (active && !column.querySelector("[data-url-panel]")) column.setAttribute("data-nav-pending", "true");
    else column.removeAttribute("data-nav-pending");
    return () => column.removeAttribute("data-nav-pending");
  }, [active]);

  return <div ref={root} className="kit-navpend" aria-hidden="true" data-active={active ? "true" : undefined} />;
}
