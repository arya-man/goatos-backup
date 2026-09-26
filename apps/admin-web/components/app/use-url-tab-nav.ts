"use client";

import { useCallback, useState, useTransition, type MouseEvent } from "react";
import { useRouter } from "next/navigation";

import { URL_NAV_EVENT, isCurrentUrl, isPlainLeftClick, type UrlNavDetail } from "./url-tab-nav";

/**
 * Navigation for URL-driven tab strips (tabs, segments, chips whose state lives in a search param).
 *
 * The click becomes `router.push(href, { scroll: false })` inside a React transition, so the router
 * keeps the current page on screen (header, crumbs, strip, filters and the old panel) until the new
 * one is ready, instead of a route skeleton. `pendingValue` is the tab just clicked while that
 * transition runs: the strip draws it as selected at once. The strip still renders real links, and
 * modified clicks are left to the browser.
 */
export function useUrlTabNav() {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const [target, setTarget] = useState<string | null>(null);

  const navigate = useCallback(
    (event: MouseEvent<HTMLElement>, value: string, href: string) => {
      if (!isPlainLeftClick(event)) return;
      event.preventDefault();
      if (isCurrentUrl(href, window.location)) return;
      setTarget(value);
      window.dispatchEvent(new CustomEvent<UrlNavDetail>(URL_NAV_EVENT, { detail: { value, href } }));
      startTransition(() => {
        router.push(href, { scroll: false });
      });
    },
    [router],
  );

  return { pendingValue: isPending ? target : null, isPending, navigate };
}
