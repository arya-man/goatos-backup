"use client";

import { useEffect, type RefObject } from "react";

/**
 * The three things every overlay on this page owes a PHONE reader, in one place: Escape,
 * a body scroll lock, and a focus trap.
 *
 * WHY THIS EXISTS. /tasks is opened at phone width inside the WhatsApp in-app browser. Without a
 * scroll lock, a drag inside the filter sheet or the edit modal scrolls the LIST behind it, so the
 * overlay appears to ignore the gesture; without a trap, Tab (and an assistive cursor) walks out of
 * the overlay into the page it is covering while the overlay still hides it.
 *
 * The scroll-lock mechanism is deliberately the one already used elsewhere in this app
 * (`features/preventive-care-vaccination/vaccination-filter-modal.tsx`,
 * `features/ceo-ai/ceo-ai-panel.tsx`, `features/herd-signals/herd-signals-mapping-table.tsx`):
 * save `document.body.style.overflow`, set `hidden`, restore the saved value on close. There is no
 * shared scroll-lock helper in this workspace, and inventing a second mechanism (a `position:fixed`
 * body, a CSS class) would be a competing one. There is no focus-trap precedent at all, so this is
 * the first — hence a hook the page's overlays share rather than a copy per overlay.
 */
const FOCUSABLE = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled]):not([type=hidden])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

export function useDialogShell({
  open,
  onClose,
  containerRef,
}: {
  open: boolean;
  onClose: () => void;
  /** The overlay's own root. Focus is kept inside it while `open`. */
  containerRef: RefObject<HTMLElement | null>;
}) {
  useEffect(() => {
    if (!open) return undefined;

    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
        return;
      }
      if (event.key !== "Tab") return;
      const container = containerRef.current;
      if (!container) return;
      // Only elements that are actually painted: the sheet's controls exist in the DOM on a wide
      // viewport too, and a hidden one must never swallow the ring.
      const items = Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
        (element) => element.offsetParent !== null,
      );
      const first = items[0];
      const last = items[items.length - 1];
      if (!first || !last) return;
      const active = document.activeElement;
      const inside = active instanceof Node && container.contains(active);
      if (event.shiftKey) {
        if (!inside || active === first) {
          event.preventDefault();
          last.focus();
        }
        return;
      }
      if (!inside || active === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey);

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = previousOverflow;
    };
  }, [open, onClose, containerRef]);
}
