"use client";

// telemetry:exempt presentational link; the page it opens carries its own route telemetry

import type { ReactNode } from "react";
import { useSyncExternalStore } from "react";

import Link from "@/components/no-prefetch-link";
import { LOCAL_OVERLAY_URL_CHANGE_EVENT } from "@/components/local-overlay-link";
import { liveQueryHref } from "./sales-park-scope";

/**
 * A same-page link whose query is built from the URL AS IT IS NOW, not as the server last rendered
 * it (defect 2026-09-25): Farm value's Over 35 kg card applies its margin in place and moves the
 * URL with replaceLocalOverlayUrl, so a farm chip rendered with the server's search params dropped
 * the applied margin on the next click. Every in-page parameter written that way is carried; the
 * link's own `patch` is applied on top. Before hydration (and on the server) the server-built
 * `fallbackHref` is used, which is the same URL until something moves it client-side.
 */
function subscribe(onChange: () => void): () => void {
  window.addEventListener("popstate", onChange);
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  return () => {
    window.removeEventListener("popstate", onChange);
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  };
}

export function LiveQueryLink({
  pagePath,
  patch,
  fallbackHref,
  className,
  current,
  children,
}: {
  pagePath: string;
  patch: Record<string, string | null>;
  fallbackHref: string;
  className?: string;
  current?: boolean;
  children: ReactNode;
}) {
  const search = useSyncExternalStore(
    subscribe,
    () => window.location.search,
    () => null,
  );
  const href = search == null ? fallbackHref : liveQueryHref(pagePath, search, patch);
  return (
    <Link href={href} scroll={false} className={className} aria-current={current ? "true" : undefined}>
      {children}
    </Link>
  );
}
