"use client";

// telemetry:exempt presentational pending-navigation feedback — no user action, no data read

import { useEffect, useRef, useState } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { RouteSkeleton } from "@/components/route-skeleton";

/** The page keeps its content, dimmed, for this long; past it the route skeleton replaces the body. */
export const LINK_NAV_SKELETON_AFTER_MS = 300;
/** A navigation that never lands (a redirect elsewhere, a failed fetch) stops looking pending after this. */
const LINK_NAV_GIVE_UP_MS = 8000;

type Pending = { href: string; navKey: string; startedAt: number };

function navKeyOf(pathname: string | null, search: string): string {
  return `${pathname ?? "/"}?${search}`;
}

/**
 * Feedback for every LINK-driven strip (kit `AnimatedTabs` with hrefs, `SegmentedLinks`).
 *
 * Those strips navigate on the server: the click is followed by a 0.5–2s round trip in which the
 * old page stays exactly as it was. The strip itself moves its indicator at once and shows the
 * progress line, and both dispatch `metricseg:navigate`; this component, mounted once in the shell
 * beside the page, turns that event into page-level feedback: `data-nav-pending` on the page
 * column dims the body and blocks its pointer (app/frame.css), and past 300ms `data-nav-skeleton`
 * hides the body and shows the route's own loading skeleton in its place — the same one a hard
 * load of the route paints — until the URL catches up. It clears the moment the router lands on
 * ANY new URL (a redirect counts as landing), and gives up after 8s so a failed navigation never
 * leaves the page dimmed.
 *
 * It renders no copy of its own.
 */
export function LinkNavPending() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const navKey = navKeyOf(pathname, searchParams?.toString() ?? "");
  const [pending, setPending] = useState<Pending | null>(null);
  // The navigation that has been in flight past the skeleton threshold, by the URL it left from.
  const [slowFrom, setSlowFrom] = useState<string | null>(null);
  const root = useRef<HTMLDivElement | null>(null);
  // Pending only while the URL is still the one the click left from: the moment the router lands
  // anywhere (the target, or a redirect), both flags are inert without any state to clear. A stale
  // record is overwritten by the next click.
  const active = pending !== null && pending.navKey === navKey;
  const slow = active && slowFrom === pending.navKey;

  useEffect(() => {
    const onNavigate = (event: Event) => {
      const detail = (event as CustomEvent<{ href?: string }>).detail;
      const href = detail?.href;
      if (!href) return;
      // A click on the tab already selected is not a navigation.
      let target: string;
      try {
        const url = new URL(href, window.location.origin);
        target = navKeyOf(url.pathname, url.searchParams.toString());
      } catch {
        return;
      }
      const current = navKeyOf(window.location.pathname, new URLSearchParams(window.location.search).toString());
      if (target === current) return;
      setPending({ href, navKey: current, startedAt: Date.now() });
    };
    window.addEventListener("metricseg:navigate", onNavigate);
    return () => window.removeEventListener("metricseg:navigate", onNavigate);
  }, []);

  useEffect(() => {
    if (!active) return undefined;
    const from = pending.navKey;
    const skeletonTimer = window.setTimeout(() => setSlowFrom(from), LINK_NAV_SKELETON_AFTER_MS);
    const giveUp = window.setTimeout(() => setPending((prev) => (prev && prev.navKey === from ? null : prev)), LINK_NAV_GIVE_UP_MS);
    return () => {
      window.clearTimeout(skeletonTimer);
      window.clearTimeout(giveUp);
    };
  }, [active, pending]);

  // The flags live on the page column (this component's parent), so the CSS can reach the page
  // root and its strips as siblings without the page knowing this component exists.
  useEffect(() => {
    const column = root.current?.parentElement;
    if (!column) return undefined;
    if (active) column.setAttribute("data-nav-pending", "true");
    else column.removeAttribute("data-nav-pending");
    if (active && slow) column.setAttribute("data-nav-skeleton", "true");
    else column.removeAttribute("data-nav-skeleton");
    return () => {
      column.removeAttribute("data-nav-pending");
      column.removeAttribute("data-nav-skeleton");
    };
  }, [active, slow]);

  return (
    <div ref={root} className="kit-navpend" aria-hidden={!(active && slow)} data-active={active ? "true" : undefined}>
      {active && slow ? (
        <div className="kit-navpend-skel" aria-live="polite" aria-busy="true">
          <RouteSkeleton />
        </div>
      ) : null}
    </div>
  );
}
