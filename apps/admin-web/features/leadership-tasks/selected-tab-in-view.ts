"use client";

import { useEffect, type RefObject } from "react";

/**
 * Keeps a scrollable MUI tab strip's SELECTED tab in view on a phone, where the strip shows about
 * three tabs and a deep link to a later one (/tasks?filter=overdue, /leave?status=approved) opened
 * with no tab visibly selected (PR #294 P4).
 *
 * Applied once a frame after render and again whenever the strip's scroller changes size: the first
 * pass alone did not take, because the strip settles its width (scroll arrows, fonts, hydration)
 * after the effect has run. Only the strip scrolls, never the page; a reader's own scroll does not
 * resize the scroller, so it is never fought.
 */
export function useSelectedTabInView(rootRef: RefObject<HTMLElement | null>, selectedKey: string) {
  useEffect(() => {
    const root = rootRef.current;
    const scroller = root?.querySelector<HTMLElement>(".MuiTabs-scroller");
    if (!scroller) return;
    const apply = () => {
      const tab = scroller.querySelector<HTMLElement>('[aria-current="true"], .Mui-selected');
      if (!tab) return;
      const tabBox = tab.getBoundingClientRect();
      const stripBox = scroller.getBoundingClientRect();
      const left = scroller.scrollLeft + (tabBox.left - stripBox.left) - (scroller.clientWidth - tabBox.width) / 2;
      scroller.scrollLeft = Math.max(0, left);
    };
    const frame = requestAnimationFrame(apply);
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(apply);
    observer?.observe(scroller);
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
    };
  }, [rootRef, selectedKey]);
}
