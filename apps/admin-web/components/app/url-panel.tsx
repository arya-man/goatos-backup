"use client";

// telemetry:exempt presentational pending state for a URL-keyed panel — no user action, no data read

import { useEffect, useState, type ReactNode } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import Box from "@mui/material/Box";

import { URL_NAV_EVENT, changesWatchedParams, type UrlNavDetail } from "@/components/app/url-tab-nav";

/** A navigation that never lands (a redirect elsewhere, a failed fetch) stops looking pending after this. */
const GIVE_UP_MS = 8000;

function navKeyOf(pathname: string | null, search: string): string {
  return `${pathname ?? "/"}?${search}`;
}

/**
 * The client half of `UrlSuspense`. The moment a navigation that changes one of this panel's
 * `watch` params starts (a tab/chip/pager link click on this page, or an announced filter change),
 * the panel shows its skeleton, before the server has answered. When the URL lands, the server's
 * keyed Suspense boundary takes over (its fallback, then the streamed content). Only this panel
 * changes: header, crumbs, tabs and filters stay mounted.
 */
export function UrlPanel({ watch, fallback, children }: { watch: readonly string[]; fallback: ReactNode; children: ReactNode }) {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const here = navKeyOf(pathname, searchParams?.toString() ?? "");
  const [pendingFrom, setPendingFrom] = useState<string | null>(null);
  const pending = pendingFrom !== null && pendingFrom === here;
  const watchKey = watch.join("|");

  useEffect(() => {
    const keys = watchKey.split("|");
    const current = () => navKeyOf(window.location.pathname, new URLSearchParams(window.location.search).toString());
    const start = (href: string | null | undefined) => {
      if (href && changesWatchedParams(href, window.location, keys)) setPendingFrom(current());
    };
    const onNavigate = (event: Event) => start((event as CustomEvent<Partial<UrlNavDetail>>).detail?.href);
    // Plain link clicks on this page (pagers, chips, tab links) start a navigation too.
    const onClick = (event: MouseEvent) => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const anchor = (event.target as Element | null)?.closest?.("a[href]");
      if (!anchor || anchor.getAttribute("target") === "_blank" || anchor.hasAttribute("download")) return;
      start(anchor.getAttribute("href"));
    };
    window.addEventListener(URL_NAV_EVENT, onNavigate);
    document.addEventListener("click", onClick, true);
    return () => {
      window.removeEventListener(URL_NAV_EVENT, onNavigate);
      document.removeEventListener("click", onClick, true);
    };
  }, [watchKey]);

  useEffect(() => {
    if (!pending) return undefined;
    const from = pendingFrom;
    const timer = window.setTimeout(() => setPendingFrom((prev) => (prev === from ? null : prev)), GIVE_UP_MS);
    return () => window.clearTimeout(timer);
  }, [pending, pendingFrom]);

  return (
    <Box data-url-panel="" aria-busy={pending || undefined} sx={{ display: "contents" }}>
      {pending ? fallback : children}
    </Box>
  );
}
