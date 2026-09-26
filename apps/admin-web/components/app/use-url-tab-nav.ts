"use client";

import { useCallback, useState, useTransition, type MouseEvent } from "react";
import { useRouter } from "next/navigation";

import { announceUrlNav, isCurrentUrl, isPlainLeftClick } from "./url-tab-nav";

/**
 * Navigation for URL-driven tab strips (tabs, segments, chips whose state lives in a search param).
 *
 * The click becomes `router.push(href, { scroll: false })`: the header, crumbs, strip and filters stay
 * mounted, the page's URL-keyed panels (`UrlSuspense`) swap to their skeleton at once on the
 * announced navigation and stream the new content in. `pendingValue` is the tab just clicked while
 * the navigation runs: the strip draws it as selected at once. The strip still renders real links, and
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
      announceUrlNav(href, value);
      startTransition(() => {
        router.push(href, { scroll: false });
      });
    },
    [router],
  );

  return { pendingValue: isPending ? target : null, isPending, navigate };
}
