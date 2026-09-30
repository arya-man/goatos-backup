"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

import Box from "@mui/material/Box";
import { varAlpha } from "minimal-shared/utils";

// Every horizontal scroller in the app: kit wrappers, the legacy per-route ones, and any element
// that opts in with `data-scroll-x`.
const SELECTOR = ".tablewrap, .twrap, .tblwrap, .feed-scroll, .kit-scroll-x, [data-scroll-x], .kit-tabs, .subtabs";
const FADE = 64;
// The template Card radius (theme shape x 2), so a fade stops at the card's rounded corner.
const CARD_RADIUS = "var(--r-xl)";

type Edge = { key: string; left: number; top: number; height: number; side: "left" | "right"; tone: "paper" | "bg"; radius: string };

function scrollParent(el: HTMLElement): HTMLElement | null {
  let node = el.parentElement;
  while (node && node !== document.body) {
    const o = getComputedStyle(node).overflowY;
    if (o === "auto" || o === "scroll") return node;
    node = node.parentElement;
  }
  return null;
}

/**
 * Right/left fade over EVERY horizontal scroller that still has content in that direction — the
 * scroll hint the frame spec asks for on tables and tab strips.
 *
 * The fades are OVERLAYS in a body portal placed from each scroller's rect. The scroller's own
 * DOM is never touched (marking it with an attribute before its page segment hydrated produced a
 * hydration mismatch on every table route). One instance in the shell covers every route:
 *  - every matching element, wherever it sits on the page, gets its edges (off-screen ones are
 *    positioned off-screen and slide in as the page scrolls);
 *  - each scroller is watched by a ResizeObserver (content/width changes) and an
 *    IntersectionObserver (entering view), the document by a MutationObserver (late mounts:
 *    tabs, drawers, streamed segments), and everything re-measures on any scroll (capture) and
 *    on resize. Fades are clipped to the scroller's own scroll container so they never paint over
 *    the top bar. Pointer-events: none.
 */
export function ScrollEdges() {
  const [edges, setEdges] = useState<Edge[]>([]);

  useEffect(() => {
    let frame = 0;
    const tracked = new Set<HTMLElement>();
    const ro = new ResizeObserver(() => schedule());
    const io = new IntersectionObserver(() => schedule(), { threshold: [0, 0.01, 0.5, 1] });

    const place = () => {
      frame = 0;
      const next: Edge[] = [];
      let n = 0;
      for (const el of document.querySelectorAll<HTMLElement>(SELECTOR)) {
        if (!tracked.has(el)) {
          tracked.add(el);
          ro.observe(el);
          io.observe(el);
          el.addEventListener("scroll", schedule, { passive: true });
        }
        if (el.scrollWidth <= el.clientWidth + 1) continue;
        const box = el.getBoundingClientRect();
        if (box.height < 24 || box.width < 48) continue;
        // Clip to the scroll container (the shell's main column, a drawer body) so a table scrolled
        // under the top bar does not paint its fade over the bar.
        const container = scrollParent(el)?.getBoundingClientRect();
        const top = container ? Math.max(box.top, container.top) : box.top;
        const bottom = container ? Math.min(box.bottom, container.bottom) : box.bottom;
        const height = Math.max(0, bottom - top);
        // A tab strip sits on the page background; a table sits on its card. The fade is the
        // colour of what is underneath and stops at the card's rounded corners.
        const isTabs = el.classList.contains("kit-tabs") || el.classList.contains("subtabs");
        const card = isTabs ? null : el.closest<HTMLElement>(".card, .MuiCard-root, .kit-tablecard");
        const c = card?.getBoundingClientRect();
        const topCorner = c ? Math.abs(c.top - box.top) < 2 && top === box.top : false;
        const bottomCorner = c ? Math.abs(c.bottom - box.bottom) < 2 && bottom === box.bottom : false;
        const radius = (side: "left" | "right") =>
          side === "right"
            ? `0 ${topCorner ? CARD_RADIUS : "0"} ${bottomCorner ? CARD_RADIUS : "0"} 0`
            : `${topCorner ? CARD_RADIUS : "0"} 0 0 ${bottomCorner ? CARD_RADIUS : "0"}`;
        const tone = isTabs ? "bg" : "paper";
        const id = `${n++}`;
        if (el.scrollLeft + el.clientWidth < el.scrollWidth - 1) next.push({ key: `${id}:r`, left: box.right - FADE, top, height, side: "right", tone, radius: radius("right") });
        if (el.scrollLeft > 1) next.push({ key: `${id}:l`, left: box.left, top, height, side: "left", tone, radius: radius("left") });
      }
      setEdges((prev) => (sameEdges(prev, next) ? prev : next));
    };
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(place);
    };
    schedule();
    const mo = new MutationObserver(schedule);
    mo.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["class", "style", "hidden", "open"] });
    window.addEventListener("scroll", schedule, { capture: true, passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      mo.disconnect();
      ro.disconnect();
      io.disconnect();
      window.removeEventListener("scroll", schedule, { capture: true });
      window.removeEventListener("resize", schedule);
      for (const el of tracked) el.removeEventListener("scroll", schedule);
    };
  }, []);

  if (edges.length === 0 || typeof document === "undefined") return null;
  return createPortal(
    <Box aria-hidden="true" sx={{ position: "fixed", inset: 0, pointerEvents: "none", zIndex: 5 }}>
      {edges.map((edge) => (
        <Box
          key={edge.key}
          component="span"
          data-visible={edge.height > 0 ? "true" : "false"}
          sx={(theme) => {
            const fade = edge.tone === "bg" ? theme.vars.palette.background.default : theme.vars.palette.background.paper;
            const channel = edge.tone === "bg" ? theme.vars.palette.background.defaultChannel : theme.vars.palette.background.paperChannel;
            return {
              position: "fixed",
              width: FADE,
              pointerEvents: "none",
              left: edge.left,
              top: edge.top,
              height: edge.height,
              borderRadius: edge.radius,
              transition: theme.transitions.create("opacity", { duration: theme.transitions.duration.shorter }),
              background: `linear-gradient(${edge.side === "right" ? 90 : 270}deg, ${varAlpha(channel, 0)}, ${varAlpha(channel, 0.92)} 65%, ${fade})`,
              boxShadow: `inset ${edge.side === "right" ? -1 : 1}px 0 0 ${theme.vars.palette.divider}`,
            };
          }}
        />
      ))}
    </Box>,
    document.body,
  );
}

function sameEdges(a: Edge[], b: Edge[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((e, i) => e.key === b[i].key && e.left === b[i].left && e.top === b[i].top && e.height === b[i].height && e.side === b[i].side && e.radius === b[i].radius);
}
