"use client";

// telemetry:exempt presentational pending state for a URL-keyed panel — no user action, no data read

import { useEffect, useRef, useState, type ReactNode } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import Box from "@mui/material/Box";

import { URL_NAV_EVENT, changesWatchedParams, type UrlNavDetail } from "@/components/app/url-tab-nav";

/** A navigation that never lands (a redirect elsewhere, a failed fetch) stops looking pending after this. */
const GIVE_UP_MS = 8000;
/**
 * J3 P0-1 (root cause): the skeleton used to wait for a 30ms timer after the click. Under load the
 * router transition kept the main thread busy past that timer, so the panel went `aria-busy` but
 * never showed its skeleton: the old /people directory stayed under the Notifications tab for the
 * whole wait (380ms here, 2.7s at 4x CPU). The skeleton now commits in the SAME urgent update as
 * the pending state, with no timer between the click and the shimmer (Ravi: "just switch and show
 * shimmer"). guard: url-panel-no-timer (components/app/url-panel.test.mjs) + r2 stale-panel.
 */

/**
 * FIXJ4 (/operations/audit "tab moved 108px", "actor filter scrolled 562px"; r2 interact
 * scroll-jump / fallback-jump, guard `url-panel-holds-page-height`): while a scrolled page swaps a
 * panel to its skeleton and back, one commit can leave the document shorter than the scroll offset
 * for a layout (a revealed Suspense boundary is still hidden while its fallback is gone), and the
 * browser clamps the page to the top. The page keeps the height it had at the click until every
 * pending panel has painted its content; then the floor is released, so a shorter result only
 * clamps the page to its new bottom (the allowed case). One shared floor for every panel.
 */
let heldPanels = 0;
let releaseFrame = 0;
function holdPageHeight(): void {
  if (typeof document === "undefined") return;
  if (heldPanels++ === 0 && window.scrollY > 0) {
    window.cancelAnimationFrame(releaseFrame);
    document.body.style.minHeight = `${document.documentElement.scrollHeight}px`;
  }
}
function releasePageHeight(): void {
  if (typeof document === "undefined" || heldPanels === 0) return;
  heldPanels -= 1;
  if (heldPanels > 0) return;
  // Two frames after the last panel painted, so the floor never lifts inside a commit.
  releaseFrame = window.requestAnimationFrame(() => {
    releaseFrame = window.requestAnimationFrame(() => {
      if (heldPanels === 0) document.body.style.minHeight = "";
    });
  });
}

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
  const [targetValue, setTargetValue] = useState<string | null>(null);
  const shapeParam = fallbackBy?.param ?? "";
  const pending = pendingFrom !== null && pendingFrom === here;
  const showFallback = pending;
  // The ROUTER's URL, not window.location: a canonical rewrite (`history.replaceState` with Next's
  // own state, components/canonical-url.tsx) moves window.location without moving useSearchParams,
  // and a key taken from window.location would then never match `here` (the weighing hang).
  const hereRef = useRef(here);
  const panelRef = useRef<HTMLDivElement | null>(null);
  const holdingRef = useRef(false);
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
          // "a|b" = any of these params set to "1" picks shape "1" (one predicate over several
          // params, e.g. the SOP editor opens on compose=1 OR new=1), else "".
          const target = new URL(href, window.location.href).searchParams;
          const names = shapeParam.split("|");
          value = names.length > 1 ? (names.some((name) => target.get(name) === "1") ? "1" : "") : (target.get(shapeParam) ?? "");
        } catch {
          value = null;
        }
      }
      const from = hereRef.current;
      // A control that calls router.push INSIDE its own startTransition announces from within that
      // transition; a state update made there would wait for the navigation to finish (the hang).
      // A microtask runs after the transition scope closes, so the skeleton is an urgent update.
      if (!holdingRef.current) {
        holdingRef.current = true;
        holdPageHeight();
      }
      queueMicrotask(() => {
        setTargetValue(value);
        setPendingFrom(from);
      });
    };
    const onNavigate = (event: Event) => start((event as CustomEvent<Partial<UrlNavDetail>>).detail?.href);
    // Plain link clicks on this page (pagers, chips, tab links) start a navigation too.
    const onClick = (event: MouseEvent) => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      // The anchor from the event PATH, not `target.closest`: a pager arrow swaps its icon for a
      // spinner inside the Link's own click handler (useLinkStatus), so by the time this bubble
      // listener runs the tapped <svg> is detached and `closest("a[href]")` finds nothing; the
      // panel never showed its skeleton and the server's fallback landed late, under the thumb
      // (FIXJ11, J3B N-P1-2 /people pager). guard: url-panel-click-path (url-panel.test.mjs).
      const anchor = event.composedPath().find((node): node is HTMLAnchorElement => node instanceof HTMLAnchorElement && node.hasAttribute("href"));
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
    // BUBBLE phase on window, i.e. AFTER React (its root listens on document) has dispatched the
    // click: a capture listener flipped this panel to its skeleton first, which unmounted a link
    // INSIDE the panel (a strip shortcut, an operator row, a pager) before next/link saw the click,
    // so the browser followed the href as a full document reload (/operations/audit). guard:
    // url-panel-click-after-react (components/app/url-panel.test.mjs) + r2 interact full-reload.
    window.addEventListener("click", onClick);
    window.addEventListener("submit", onSubmit);
    return () => {
      window.removeEventListener(URL_NAV_EVENT, onNavigate);
      window.removeEventListener("click", onClick);
      window.removeEventListener("submit", onSubmit);
    };
  }, [watchKey, ignoreKey, shapeParam]);

  // Release the page-height floor once the landed panel shows content (no skeleton left in it),
  // or after the give-up time.
  useEffect(() => {
    if (pending || !holdingRef.current) return undefined;
    const started = performance.now();
    let frame = 0;
    const check = () => {
      const el = panelRef.current;
      const settled = !el || !el.querySelector(".MuiSkeleton-root, [data-skel]");
      if (settled || performance.now() - started > GIVE_UP_MS) {
        holdingRef.current = false;
        releasePageHeight();
        return;
      }
      frame = window.requestAnimationFrame(check);
    };
    frame = window.requestAnimationFrame(check);
    return () => window.cancelAnimationFrame(frame);
  }, [pending, here]);
  useEffect(
    () => () => {
      if (holdingRef.current) {
        holdingRef.current = false;
        releasePageHeight();
      }
    },
    [],
  );

  useEffect(() => {
    if (!pending) return undefined;
    const from = pendingFrom;
    const giveUp = window.setTimeout(() => setPendingFrom((prev) => (prev === from ? null : prev)), GIVE_UP_MS);
    return () => window.clearTimeout(giveUp);
  }, [pending, pendingFrom]);

  // `display: contents` makes the panel's children the page grid's items, so the page root's
  // `> * { min-width: 0 }` never reaches them. Without it a wide child (a 960px table, a chart)
  // sizes the page's one grid track to its min-content and the whole page — header, KPI row,
  // toolbar — overflows the content column (TR1-#5, /sales/loads 1561px in 1060px).
  // guard: url-panel-min-width
  return (
    <Box
      ref={panelRef}
      data-url-panel=""
      data-url-panel-pending={showFallback ? "" : undefined}
      aria-busy={pending || undefined}
      sx={{ display: "contents", "& > *": { minWidth: 0 } }}
    >
      {showFallback ? (targetValue !== null && fallbackBy?.shapes[targetValue]) || fallback : children}
    </Box>
  );
}
