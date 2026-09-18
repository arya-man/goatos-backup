"use client";

import { useCallback, useEffect, useRef, type ReactNode } from "react";

/**
 * The drawer the task detail slides in from the right edge, OVER the board or the table.
 *
 * WHY AN OVERLAY. The detail used to take a grid column beside the board
 * (`.lt-grid:has(.ltd-panel)` gave it 54% of the row). Four fixed-width board columns then had
 * 537px to live in at 1700 wide, so Done and Cancelled were sliced off the moment a card was
 * clicked -- the page looked broken on the reader's first interaction. A drawer leaves the board
 * at full width underneath and borrows nothing from it. This is the same `button.scrim` +
 * `aside.drawer` pair the Action Center and the audit log already use, so the drawer looks and
 * behaves like every other drawer in the console.
 *
 * WHAT THIS SHELL OWNS, AND WHAT IT DOES NOT. The URL contract is untouched: `task=<id>` opens the
 * detail and the Close link removes it, both as ordinary Next navigations rendered by the SERVER.
 * This client shell only wraps that server-rendered panel with the things a drawer owes the
 * reader: a scrim that closes on click, Escape (deferred to the Edit modal and the mention popup
 * while one of THEM is open, so each press unwinds one layer), a focus move into the drawer on
 * open and back to the card on close, a Tab loop, and a body scroll lock (the same
 * `document.body.style.overflow` mechanism as `use-dialog-shell.tsx`, not a second one).
 */
const FOCUSABLE = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled]):not([type=hidden])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

/** An overlay stacked ON TOP of the drawer (the Edit modal, the @-mention picker) owns the keys. */
function anotherOverlayIsOpen(): boolean {
  return Boolean(document.querySelector(".lt-modal[role=dialog], [data-mention-popup]"));
}

export function TaskDetailDrawer({
  taskId,
  onClose,
  ariaLabel,
  closeLabel,
  children,
}: {
  taskId: string;
  /** Pops the local overlay (history entry or URL replace); never a route navigation. */
  onClose: () => void;
  ariaLabel: string;
  closeLabel: string;
  children: ReactNode;
}) {
  const drawerRef = useRef<HTMLElement>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  const close = useCallback(() => {
    onClose();
  }, [onClose]);

  useEffect(() => {
    const drawer = drawerRef.current;
    // The card (or table row) that opened the drawer still holds focus after the soft navigation;
    // it is remembered so Close hands focus back to it instead of dropping it on <body>.
    const active = document.activeElement;
    openerRef.current =
      active instanceof HTMLElement && active.closest(".lt-grid") ? active : null;
    // Focus lands on the drawer itself (tabIndex -1) so a screen reader announces the dialog and
    // the next Tab is its first control. Re-asserted a frame later and once more after the router
    // has settled: the App Router's own post-navigation focus handling can run AFTER this effect
    // and drop focus back on <body>, which would leave a keyboard reader behind the scrim.
    const focusDrawer = () => {
      if (!drawer || !drawer.isConnected) return;
      const current = document.activeElement;
      if (current instanceof Node && drawer.contains(current) && current !== drawer) return;
      if (current !== drawer) drawer.focus({ preventScroll: true });
    };
    focusDrawer();
    const focusFrame = window.requestAnimationFrame(focusDrawer);
    const focusTimer = window.setTimeout(focusDrawer, 150);

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || anotherOverlayIsOpen()) return;
      if (event.key === "Escape") {
        event.preventDefault();
        close();
        return;
      }
      if (event.key !== "Tab" || !drawer) return;
      const items = Array.from(drawer.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
        (element) => element.offsetParent !== null,
      );
      const first = items[0];
      const last = items[items.length - 1];
      if (!first || !last) return;
      const current = document.activeElement;
      const inside = current instanceof Node && drawer.contains(current);
      if (event.shiftKey) {
        if (!inside || current === first || current === drawer) {
          event.preventDefault();
          last.focus();
        }
        return;
      }
      if (!inside || current === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey);

    return () => {
      window.cancelAnimationFrame(focusFrame);
      window.clearTimeout(focusTimer);
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = previousOverflow;
      const opener = openerRef.current;
      const target =
        opener && opener.isConnected
          ? opener
          : document.querySelector<HTMLElement>(`.lt-grid a[href*="task=${CSS.escape(taskId)}"]`);
      target?.focus({ preventScroll: true });
    };
  }, [close, taskId]);

  return (
    <>
      <button type="button" className="scrim on ltd-scrim" aria-label={closeLabel} onClick={close} />
      <aside
        ref={drawerRef}
        className="drawer on ltd-drawer"
        role="dialog"
        aria-modal="true"
        aria-label={ariaLabel}
        tabIndex={-1}
        data-task-drawer={taskId}
      >
        {children}
      </aside>
    </>
  );
}
