"use client";

// telemetry:exempt presentational pending state for a URL-keyed panel — no user action, no data read

import { useEffect, useRef, useState, type ReactNode } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import Box from "@mui/material/Box";

import { URL_NAV_EVENT, changesWatchedParams, type UrlNavDetail } from "@/components/app/url-tab-nav";

/** A navigation that never lands (a redirect elsewhere, a failed fetch) stops looking pending after this. */
const GIVE_UP_MS = 8000;
/**
 * The skeleton appears this long after the click when the answer has not landed yet. An answer
 * that is already cached (router cache, back/forward) lands inside it, so it shows directly with no
 * skeleton flash; anything slower is a skeleton well inside the 100ms a click may look idle.
 */
export const URL_PANEL_SKELETON_DELAY_MS = 30;

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
export function UrlPanel({
  watch,
  ignore = [],
  fallback,
  fallbackBy,
  children,
}: {
  watch: readonly string[];
  ignore?: readonly string[];
  fallback: ReactNode;
  /** A skeleton per value of one param (a tab strip whose tabs have different shapes): the clicked tab's shape shows. */
  fallbackBy?: { param: string; shapes: Record<string, ReactNode> };
  children: ReactNode;
}) {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const here = navKeyOf(pathname, searchParams?.toString() ?? "");
  const [pendingFrom, setPendingFrom] = useState<string | null>(null);
  const [shownFor, setShownFor] = useState<string | null>(null);
  const [targetValue, setTargetValue] = useState<string | null>(null);
  const shapeParam = fallbackBy?.param ?? "";
  const pending = pendingFrom !== null && pendingFrom === here;
  const showFallback = pending && shownFor === pendingFrom;
  // The ROUTER's URL, not window.location: a canonical rewrite (`history.replaceState` with Next's
  // own state, components/canonical-url.tsx) moves window.location without moving useSearchParams,
  // and a key taken from window.location would then never match `here` (the weighing hang).
  const hereRef = useRef(here);
  useEffect(() => {
    hereRef.current = here;
  }, [here]);
  const watchKey = watch.join("|");
  const ignoreKey = ignore.join("|");

  useEffect(() => {
    const keys = watchKey.split("|");
    const skip = ignoreKey ? ignoreKey.split("|") : [];
    const start = (href: string | null | undefined) => {
      if (!href || !changesWatchedParams(href, window.location, keys, skip)) return;
      let value: string | null = null;
      if (shapeParam) {
        try {
          value = new URL(href, window.location.href).searchParams.get(shapeParam) ?? "";
        } catch {
          value = null;
        }
      }
      const from = hereRef.current;
      // A control that calls router.push INSIDE its own startTransition announces from within that
      // transition; a state update made there would wait for the navigation to finish (the hang).
      // A microtask runs after the transition scope closes, so the skeleton is an urgent update.
      queueMicrotask(() => {
        setTargetValue(value);
        setPendingFrom(from);
      });
    };
    const onNavigate = (event: Event) => start((event as CustomEvent<Partial<UrlNavDetail>>).detail?.href);
    // Plain link clicks on this page (pagers, chips, tab links) start a navigation too.
    const onClick = (event: MouseEvent) => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const anchor = (event.target as Element | null)?.closest?.("a[href]");
      if (!anchor || anchor.getAttribute("target") === "_blank" || anchor.hasAttribute("download")) return;
      start(anchor.getAttribute("href"));
    };
    // GET search forms (`next/form`) navigate to their action + fields.
    const onSubmit = (event: SubmitEvent) => {
      const form = event.target as HTMLFormElement | null;
      if (!form || form.tagName !== "FORM" || (form.getAttribute("method") ?? "get").toLowerCase() !== "get") return;
      try {
        const url = new URL(form.getAttribute("action") || window.location.pathname, window.location.href);
        url.search = "";
        for (const [key, value] of new FormData(form, event.submitter ?? undefined)) if (typeof value === "string") url.searchParams.append(key, value);
        start(`${url.pathname}${url.search}`);
      } catch {
        /* not a URL form */
      }
    };
    window.addEventListener(URL_NAV_EVENT, onNavigate);
    document.addEventListener("click", onClick, true);
    document.addEventListener("submit", onSubmit, true);
    return () => {
      window.removeEventListener(URL_NAV_EVENT, onNavigate);
      document.removeEventListener("click", onClick, true);
      document.removeEventListener("submit", onSubmit, true);
    };
  }, [watchKey, ignoreKey, shapeParam]);

  useEffect(() => {
    if (!pending) return undefined;
    const from = pendingFrom;
    const show = window.setTimeout(() => setShownFor(from), URL_PANEL_SKELETON_DELAY_MS);
    const giveUp = window.setTimeout(() => setPendingFrom((prev) => (prev === from ? null : prev)), GIVE_UP_MS);
    return () => {
      window.clearTimeout(show);
      window.clearTimeout(giveUp);
    };
  }, [pending, pendingFrom]);

  // `display: contents` makes the panel's children the page grid's items, so the page root's
  // `> * { min-width: 0 }` never reaches them. Without it a wide child (a 960px table, a chart)
  // sizes the page's one grid track to its min-content and the whole page — header, KPI row,
  // toolbar — overflows the content column (TR1-#5, /sales/loads 1561px in 1060px).
  // guard: url-panel-min-width
  return (
    <Box
      data-url-panel=""
      data-url-panel-pending={showFallback ? "" : undefined}
      aria-busy={pending || undefined}
      sx={{ display: "contents", "& > *": { minWidth: 0 } }}
    >
      {showFallback ? (targetValue !== null && fallbackBy?.shapes[targetValue]) || fallback : children}
    </Box>
  );
}
