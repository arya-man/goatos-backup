"use client";

import { useRouter } from "next/navigation";
import { useActionState, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { LocalOverlayLink } from "@/components/local-overlay-link";
import { deleteRowAction, setRowStatusAction, type ConfigurationActionState } from "./configuration-actions";

/**
 * THE ROW'S OWN ACTIONS (maintainer instruction 2026-09-22: "if you want to edit or remove one
 * pen, make it inactive, or remove one medicine -- it should be very nice, simple, easy; it's too
 * random and too messy").
 *
 * Every one of these already worked. None of them could be FOUND: the only way to reach them was
 * to click a row's name, open the drawer, scroll past a form of up to fourteen inputs, and read
 * two buttons labelled "Archive" and "Delete" -- words the farm does not use. A capability nobody
 * can find is, on screen, a capability that does not exist.
 *
 * So each row now carries the same three, in the same place, on every register: Edit, Make
 * inactive / Make active, Remove. The drawer keeps them too -- someone already inside a record
 * should not have to close it to act on it -- and both surfaces post the SAME server actions, so
 * there is one write path and one refusal sentence, not two that can drift.
 *
 * It composes no copy: every label arrives from the page contract, and a refusal is the backend's
 * own sentence (`In use by 12 animals, 3 partitions`) shown verbatim. A built-in row offers only
 * Edit, because the backend refuses the other two for it and offering a button that always fails
 * is a worse answer than not offering it.
 */

const INITIAL_ACTION_STATE: ConfigurationActionState = { status: "idle", code: "", detail: "", fields: {}, ticket: 0 };

export function RowActions({
  register,
  rowId,
  rowVersion,
  status,
  isBuiltin,
  editHref,
  canEdit,
  canSetStatus,
  canDelete,
  labels,
}: {
  register: string;
  rowId: string;
  rowVersion: number;
  status: string;
  isBuiltin: boolean;
  editHref: string;
  canEdit: boolean;
  canSetStatus: boolean;
  canDelete: boolean;
  /** Resolved copy: the page contract's words, never composed here. */
  labels: {
    edit: string;
    deactivate: string;
    activate: string;
    remove: string;
    cancel: string;
    confirmRemove: string;
    more: string;
    failed: string;
  };
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  // The menu is positioned in VIEWPORT coordinates, not inside the row. The table scrolls in a
  // `.tablewrap` with `overflow: auto`, so an absolutely positioned panel is CLIPPED by it: on the
  // last rows of a page the menu rendered as a sliver below the row and its items could not be
  // reached at all -- the same unusable state this whole change exists to remove. Measuring the
  // button and drawing the panel `position: fixed` escapes the scroll container entirely, and it
  // flips ABOVE the button when there is not enough room below.
  const [anchor, setAnchor] = useState<{ top?: number; bottom?: number; right: number } | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [statusState, statusFormAction, statusPending] = useActionState(setRowStatusAction, INITIAL_ACTION_STATE);
  const [deleteState, deleteFormAction, deletePending] = useActionState(deleteRowAction, INITIAL_ACTION_STATE);
  const holder = useRef<HTMLDivElement | null>(null);
  const trigger = useRef<HTMLButtonElement | null>(null);
  const menu = useRef<HTMLDivElement | null>(null);
  const seen = useRef(0);

  const place = useCallback(() => {
    const button = trigger.current;
    if (!button) return;
    const rect = button.getBoundingClientRect();
    const height = menu.current?.offsetHeight ?? 0;
    const below = window.innerHeight - rect.bottom;
    const right = Math.max(8, window.innerWidth - rect.right);
    // Flip up only when the panel genuinely does not fit below AND there is more room above, so a
    // menu near the top of a short window does not fly off the other edge instead.
    if (height > 0 && below < height + 12 && rect.top > below) {
      setAnchor({ bottom: Math.max(8, window.innerHeight - rect.top + 4), right });
      return;
    }
    setAnchor({ top: rect.bottom + 4, right });
  }, []);

  // A success closes the menu and re-reads the table in place; the row it acted on either changes
  // chip or disappears. Closing is state derived from the new ticket, so it happens during render
  // (React's "adjust state when an input changes"); only the refresh, which reaches outside React,
  // is an effect.
  const ticket = statusState.ticket * 1000 + deleteState.ticket;
  const succeeded = statusState.status === "success" || deleteState.status === "success";
  const [closedFor, setClosedFor] = useState(0);
  if (ticket !== closedFor) {
    setClosedFor(ticket);
    if (succeeded) {
      setOpen(false);
      setConfirming(false);
    }
  }
  useEffect(() => {
    if (ticket === seen.current) return;
    seen.current = ticket;
    if (succeeded) router.refresh();
  }, [ticket, succeeded, router]);

  // Measure before paint so the panel never appears in the wrong place for a frame.
  useLayoutEffect(() => {
    if (open) place();
  }, [open, confirming, place]);

  // Outside click and Escape close the menu, the same way every other overlay on this product does.
  // A scroll or resize RE-PLACES it rather than closing it: the browser scrolls a button near the
  // edge into view as part of clicking it, so dismissing on scroll shut the menu in the very case
  // the fixed positioning exists for. It closes only once its own row has left the viewport, where
  // a floating panel would be pointing at nothing.
  useEffect(() => {
    if (!open) return;
    const dismiss = () => {
      setOpen(false);
      setConfirming(false);
    };
    const follow = () => {
      const rect = trigger.current?.getBoundingClientRect();
      if (!rect || rect.bottom < 0 || rect.top > window.innerHeight) {
        dismiss();
        return;
      }
      place();
    };
    window.addEventListener("resize", follow);
    window.addEventListener("scroll", follow, true);
    const onDown = (event: MouseEvent) => {
      const target = event.target as Node;
      if (menu.current?.contains(target)) return;
      if (holder.current && !holder.current.contains(target)) {
        setOpen(false);
        setConfirming(false);
      }
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        setConfirming(false);
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("resize", follow);
      window.removeEventListener("scroll", follow, true);
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, place]);

  // The panel is PORTALLED to the body, not left in the row. Fixed positioning alone was not
  // enough: each row's action cell is its own positioned box, so a later row's cell painted over
  // the open panel and swallowed the click -- the menu looked right and did nothing, which is
  // worse than the clipping it replaced. Out at the body there is no ancestor to compete with.
  const offerStatus = canSetStatus && !isBuiltin;
  const offerDelete = canDelete && !isBuiltin;
  const refusal =
    (statusState.status === "error" ? statusState.detail || labels.failed : "") ||
    (deleteState.status === "error" ? deleteState.detail || labels.failed : "");

  return (
    <div className="cfg-rowacts" ref={holder}>
      {canEdit ? (
        <LocalOverlayLink href={editHref} scroll={false} className="btn sm ghost">
          {labels.edit}
        </LocalOverlayLink>
      ) : null}
      {offerStatus || offerDelete ? (
        <button
          type="button"
          ref={trigger}
          className="btn sm ghost cfg-rowacts-more"
          aria-haspopup="menu"
          aria-expanded={open}
          aria-label={labels.more}
          onClick={() => {
            setConfirming(false);
            setOpen((current) => !current);
          }}
        >
          &#8943;
        </button>
      ) : null}
      {open && typeof document !== "undefined"
        ? createPortal(
        <div
          className="cfg-rowacts-menu"
          role="menu"
          ref={menu}
          data-row-actions={rowId}
          style={{ top: anchor?.top, bottom: anchor?.bottom, right: anchor?.right ?? 0, visibility: anchor ? "visible" : "hidden" }}
        >
          {offerStatus ? (
            <form action={statusFormAction} aria-busy={statusPending}>
              <input type="hidden" name="register" value={register} />
              <input type="hidden" name="row_id" value={rowId} />
              <input type="hidden" name="row_version" value={rowVersion} />
              <input type="hidden" name="status" value={status === "archived" ? "active" : "archived"} />
              <button type="submit" role="menuitem" disabled={statusPending}>
                {status === "archived" ? labels.activate : labels.deactivate}
              </button>
            </form>
          ) : null}
          {offerDelete ? (
            confirming ? (
              <form action={deleteFormAction} aria-busy={deletePending} className="cfg-rowacts-confirm">
                <input type="hidden" name="register" value={register} />
                <input type="hidden" name="row_id" value={rowId} />
                <input type="hidden" name="row_version" value={rowVersion} />
                <p>{labels.confirmRemove}</p>
                <button type="submit" role="menuitem" className="cfg-rowacts-danger" disabled={deletePending}>
                  {labels.remove}
                </button>
                <button type="button" role="menuitem" onClick={() => setConfirming(false)}>
                  {labels.cancel}
                </button>
              </form>
            ) : (
              <button type="button" role="menuitem" className="cfg-rowacts-danger" onClick={() => setConfirming(true)}>
                {labels.remove}
              </button>
            )
          ) : null}
          {refusal ? (
            <p className="cfg-rowacts-error" role="status" aria-live="polite">
              {refusal}
            </p>
          ) : null}
        </div>,
            document.body,
          )
        : null}
    </div>
  );
}
