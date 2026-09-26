"use client";

import { useCallback, useEffect, type ReactNode } from "react";
import Drawer from "@mui/material/Drawer";

/**
 * The drawer the task detail slides in from the right edge, OVER the board or the table.
 *
 * WHY AN OVERLAY. The detail used to take a grid column beside the board
 * (`.lt-grid:has(.ltd-panel)` gave it 54% of the row). Four fixed-width board columns then had
 * 537px to live in at 1700 wide, so Done and Cancelled were sliced off the moment a card was
 * clicked -- the page looked broken on the reader's first interaction. A drawer leaves the board
 * at full width underneath and borrows nothing from it. It is the template MUI Drawer (portalled
 * above the page and the Ask Mesha button).
 *
 * WHAT THIS SHELL OWNS, AND WHAT IT DOES NOT. The URL contract is untouched: `task=<id>` opens the
 * detail and the Close link removes it, both as ordinary Next navigations rendered by the SERVER.
 * This client shell only wraps that server-rendered panel with the things a drawer owes the
 * reader, all from MUI: a scrim that closes on click, Escape (deferred to the Edit modal and the
 * mention popup while one of THEM is open, so each press unwinds one layer), focus into the
 * drawer on open and back to the card on close, a focus trap, and a body scroll lock.
 */
/** An overlay stacked ON TOP of the drawer (the Edit modal, the @-mention picker) owns the keys. */
function anotherOverlayIsOpen(): boolean {
  return Boolean(document.querySelector(".lt-modal[role=dialog], [data-mention-popup], .MuiPopover-root"));
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
  const close = useCallback(
    (event: object, reason: "backdropClick" | "escapeKeyDown") => {
      // Each Escape unwinds ONE layer: the @-mention popup (not an MUI modal) gets it first.
      if (reason === "escapeKeyDown" && ((event as KeyboardEvent).defaultPrevented || anotherOverlayIsOpen())) return;
      onClose();
    },
    [onClose],
  );

  // MUI restores focus to the card that opened the drawer. When the soft navigation re-rendered
  // that card (so the remembered node is gone), hand focus to the task's card by its link instead
  // of dropping it on <body>.
  useEffect(() => {
    return () => {
      window.requestAnimationFrame(() => {
        if (document.activeElement && document.activeElement !== document.body) return;
        document
          .querySelector<HTMLElement>(`.lt-grid a[href*="task=${CSS.escape(taskId)}"], a.ltb-card[href*="task=${CSS.escape(taskId)}"]`)
          ?.focus({ preventScroll: true });
      });
    };
  }, [taskId]);

  return (
    <Drawer
      anchor="right"
      open
      onClose={close}
      slotProps={{
        backdrop: { "aria-label": closeLabel } as object,
        paper: { className: "ltd-drawer", tabIndex: -1, "data-task-drawer": taskId, role: "dialog", "aria-modal": true, "aria-label": ariaLabel } as object,
      }}
    >
      {children}
    </Drawer>
  );
}
