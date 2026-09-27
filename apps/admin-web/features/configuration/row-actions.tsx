"use client";

import { useRouter } from "next/navigation";
import { startTransition, useActionState, useEffect, useRef, useState } from "react";
import { usePopover } from "minimal-shared/hooks";
import IconButton from "@mui/material/IconButton";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import Typography from "@mui/material/Typography";

import { CustomPopover } from "@/components/minimal/custom-popover";
import { Iconify } from "@/components/minimal/iconify";
import { TAP_MIN, phoneTapSx } from "@/components/app/tap";
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
 *
 * The overflow is the template's table-row action pattern (sections/user/user-table-row.tsx):
 * IconButton + usePopover + CustomPopover + MenuList. MUI Popover portals it to <body> (so no later
 * row's cell can paint over it), places it against the trigger inside the viewport, and closes it
 * on outside click and Escape.
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
  const menu = usePopover();
  const [confirming, setConfirming] = useState(false);
  const [statusState, statusFormAction, statusPending] = useActionState(setRowStatusAction, INITIAL_ACTION_STATE);
  const [deleteState, deleteFormAction, deletePending] = useActionState(deleteRowAction, INITIAL_ACTION_STATE);
  const seen = useRef(0);

  const close = () => {
    menu.onClose();
    setConfirming(false);
  };

  // A success closes the menu and re-reads the table in place; the row it acted on either changes
  // chip or disappears. Closing is state derived from the new ticket, so it happens during render
  // (React's "adjust state when an input changes"); only the refresh, which reaches outside React,
  // is an effect.
  const ticket = statusState.ticket * 1000 + deleteState.ticket;
  const succeeded = statusState.status === "success" || deleteState.status === "success";
  const [closedFor, setClosedFor] = useState(0);
  if (ticket !== closedFor) {
    setClosedFor(ticket);
    if (succeeded) close();
  }
  useEffect(() => {
    if (ticket === seen.current) return;
    seen.current = ticket;
    if (succeeded) router.refresh();
  }, [ticket, succeeded, router]);

  // Both writes post the same server actions the drawer posts; the row identity travels as form data.
  const rowForm = () => {
    const form = new FormData();
    form.set("register", register);
    form.set("row_id", rowId);
    form.set("row_version", String(rowVersion));
    return form;
  };
  const submitStatus = () => {
    const form = rowForm();
    form.set("status", status === "archived" ? "active" : "archived");
    startTransition(() => statusFormAction(form));
  };
  const submitDelete = () => {
    const form = rowForm();
    startTransition(() => deleteFormAction(form));
  };

  const offerStatus = canSetStatus && !isBuiltin;
  const offerDelete = canDelete && !isBuiltin;
  const refusal =
    (statusState.status === "error" ? statusState.detail || labels.failed : "") ||
    (deleteState.status === "error" ? deleteState.detail || labels.failed : "");

  return (
    <div className="cfg-rowacts">
      {canEdit ? (
        <LocalOverlayLink href={editHref} scroll={false} className="btn sm ghost">
          {labels.edit}
        </LocalOverlayLink>
      ) : null}
      {offerStatus || offerDelete ? (
        <IconButton
          aria-haspopup="menu"
          aria-expanded={menu.open}
          aria-label={labels.more}
          color={menu.open ? "inherit" : "default"}
          onClick={(event) => {
            setConfirming(false);
            menu.onOpen(event);
          }}
          sx={phoneTapSx}
        >
          <Iconify icon="eva:more-vertical-fill" />
        </IconButton>
      ) : null}
      <CustomPopover
        open={menu.open}
        anchorEl={menu.anchorEl}
        onClose={close}
        slotProps={{ arrow: { placement: "right-top" }, paper: { sx: { maxWidth: 280 } } }}
      >
        <MenuList aria-label={labels.more}>
          {offerStatus ? (
            <MenuItem disabled={statusPending} aria-busy={statusPending} onClick={submitStatus} sx={phoneTapSx}>
              {status === "archived" ? labels.activate : labels.deactivate}
            </MenuItem>
          ) : null}
          {offerDelete && confirming ? (
            <li>
              <Typography variant="caption" component="p" sx={{ px: 1, py: 0.5, color: "text.secondary" }}>
                {labels.confirmRemove}
              </Typography>
            </li>
          ) : null}
          {offerDelete ? (
            <MenuItem
              disabled={deletePending}
              aria-busy={deletePending}
              onClick={confirming ? submitDelete : () => setConfirming(true)}
              sx={(theme) => ({ color: theme.palette.error.main, [theme.breakpoints.down("sm")]: { minHeight: TAP_MIN } })}
            >
              {labels.remove}
            </MenuItem>
          ) : null}
          {offerDelete && confirming ? (
            <MenuItem onClick={() => setConfirming(false)} sx={phoneTapSx}>
              {labels.cancel}
            </MenuItem>
          ) : null}
          {refusal ? (
            <li role="status" aria-live="polite">
              <Typography variant="caption" component="p" sx={{ px: 1, py: 0.5, color: "error.main" }}>
                {refusal}
              </Typography>
            </li>
          ) : null}
        </MenuList>
      </CustomPopover>
    </div>
  );
}
