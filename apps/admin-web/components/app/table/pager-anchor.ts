"use client";

// pager-under-thumb (FIXJ11, J3B P2-2): a phone reader taps Next with the pager under the thumb.
// When the next page's rows are taller or shorter than this page's (two-line pen names, an
// expanded composition), the pager lands 60-78px away from the thumb (/counts/breakdown,
// /counts/milk-preparation) and the next tap misses. The pager remembers where it was tapped and,
// once the new page has laid out, scrolls the page by the difference so it sits where it was.
// Runtime guard: r2 `interact|Pager|pager-jump` (390, every pager route); unit: pager-anchor.test.mjs.

import { useLayoutEffect, type RefObject } from "react";

const PAGER_SEL = "[data-pager]";
/** A tap older than this never moves the page (a slow answer the reader has scrolled away from). */
const ANCHOR_TTL_MS = 10_000;

type Anchor = { index: number; count: number; top: number; href: string; at: number };
let pending: Anchor | null = null;

function pagerRoots(): Element[] {
  return [...document.querySelectorAll(PAGER_SEL)].filter((el) => !el.parentElement?.closest(PAGER_SEL));
}

/** The distance to scroll so the pager `now` sits at `top` again, or 0 (pure; unit-tested). */
export function anchorDelta(anchor: Pick<Anchor, "top" | "at"> | null, nowTop: number, now: number, viewport: number): number {
  if (!anchor || now - anchor.at > ANCHOR_TTL_MS) return 0;
  if (anchor.top < 0 || anchor.top > viewport) return 0;
  const delta = Math.round(nowTop - anchor.top);
  return Math.abs(delta) > 1 ? delta : 0;
}

/** How long after the new page mounted the pager is still held (the template Scrollbar and
 * streamed cells settle a few frames later than the commit). */
const HOLD_MS = 700;

/**
 * Put `el` back at `top` now and on every frame for HOLD_MS, until the reader scrolls themselves
 * (wheel / touch / key), so late layout (Scrollbar height, streamed rows) cannot move it either.
 */
function holdUnderThumb(el: HTMLElement, anchor: { top: number; at: number }): void {
  let stopped = false;
  const until = performance.now() + HOLD_MS;
  const correct = () => {
    if (stopped || !el.isConnected || performance.now() > until) return;
    const delta = anchorDelta(anchor, el.getBoundingClientRect().top, performance.now(), window.innerHeight);
    if (delta) window.scrollBy({ top: delta, behavior: "instant" as ScrollBehavior });
  };
  const stop = () => {
    stopped = true;
    for (const type of ["wheel", "touchstart", "keydown"] as const) window.removeEventListener(type, stop);
    window.removeEventListener("scroll", correct);
  };
  for (const type of ["wheel", "touchstart", "keydown"] as const) window.addEventListener(type, stop, { passive: true });
  // A scroll the page makes by itself during the hold (scroll anchoring over a swapped subtree)
  // is undone in the same frame, before it paints.
  window.addEventListener("scroll", correct, { passive: true });
  const step = () => {
    if (stopped || !el.isConnected) return stop();
    correct();
    if (performance.now() < until) window.requestAnimationFrame(step);
    else stop();
  };
  // First correction in the next animation frame (still before that frame paints), never in the
  // commit: the template Scrollbar (SimpleBar) above the pager is 0px tall until its own effect
  // has run, so a correction measured inside the commit scrolled /counts/breakdown by -1156px.
  window.requestAnimationFrame(step);
}

/** Record where a URL pager was tapped (its arrow's click handler). */
export function rememberPagerTap(from: Element | null): void {
  const root = from?.closest(PAGER_SEL);
  if (!root) return;
  const roots = pagerRoots();
  pending = { index: roots.indexOf(root), count: roots.length, top: root.getBoundingClientRect().top, href: window.location.href, at: performance.now() };
}

/**
 * On every render of a URL pager: if a tap on "this" pager (same index among the same number of
 * pagers) is pending, put the pager back under the thumb once and forget the tap.
 */
export function usePagerAnchor(ref: RefObject<HTMLElement | null>): void {
  useLayoutEffect(() => {
    const el = ref.current;
    // Only once the navigation has landed (the URL moved): a render while it is in flight is not the new page.
    if (!pending || !el || window.location.href === pending.href) return;
    const roots = pagerRoots();
    if (roots.length !== pending.count || roots.indexOf(el) !== pending.index) return;
    const anchor = pending;
    pending = null;
    if (performance.now() - anchor.at > ANCHOR_TTL_MS) return;
    holdUnderThumb(el, { top: anchor.top, at: performance.now() });
  });
}

/**
 * Client pagers (TableFooter): the same compensation for a page change that is a state update.
 * Call `remember()` just before changing the page; the layout effect keyed by `page` restores.
 */
export function useClientPagerAnchor(ref: RefObject<HTMLElement | null>, page: number): () => void {
  useLayoutEffect(() => {
    const el = ref.current;
    const anchor = clientAnchors.get(ref);
    if (!el || !anchor) return;
    clientAnchors.delete(ref);
    if (performance.now() - anchor.at > ANCHOR_TTL_MS) return;
    holdUnderThumb(el, { top: anchor.top, at: performance.now() });
  }, [page, ref]);
  return () => {
    const el = ref.current;
    if (el) clientAnchors.set(ref, { top: el.getBoundingClientRect().top, at: performance.now() });
  };
}
const clientAnchors = new WeakMap<object, { top: number; at: number }>();
