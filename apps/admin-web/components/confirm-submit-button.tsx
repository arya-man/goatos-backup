"use client";

import { useCallback, useRef, useState } from "react";

import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";

/**
 * Submit button that asks first.
 *
 * The old implementation called `window.confirm()` in `onClick`, which is the one dialog on the
 * product that cannot be themed, cannot be reached by the page's focus trap and looks like a
 * browser error on mobile. It is the template confirm dialog (MUI Dialog + DialogTitle/Content/Actions), with the same exported props so the call
 * sites are untouched.
 *
 * Mechanics: the visible button never submits. It opens the dialog; confirming clicks a hidden
 * `type="submit"` sibling, which submits the owning form exactly the way the old button did
 * (server actions included) without re-entering this guard.
 */
export function ConfirmSubmitButton({
  children,
  message,
  className,
  variant = "contained",
  disabled = false,
  title,
  confirmLabel,
  cancelLabel,
  dialogTitle,
}: {
  children: React.ReactNode;
  message: string;
  className?: string;
  /** Template button variant of the visible trigger (contained primary for the main action). */
  variant?: "contained" | "outlined";
  disabled?: boolean;
  title?: string;
  /** Backend copy from the caller's page contract. */
  confirmLabel: string;
  cancelLabel: string;
  dialogTitle: string;
}) {
  const [open, setOpen] = useState(false);
  const submitter = useRef<HTMLButtonElement | null>(null);

  const confirm = useCallback(() => {
    setOpen(false);
    submitter.current?.click();
  }, []);

  return (
    <>
      <Button
        type="button"
        variant={variant}
        color="primary"
        className={className}
        disabled={disabled}
        title={title}
        sx={{ alignSelf: "flex-start" }}
        onClick={() => {
          if (!disabled) setOpen(true);
        }}
      >
        {children}
      </Button>
      <button ref={submitter} type="submit" hidden aria-hidden="true" tabIndex={-1} />
      <Dialog fullWidth maxWidth="xs" open={open} onClose={() => setOpen(false)} slotProps={{ paper: { "aria-label": dialogTitle } }}>
          <DialogTitle sx={{ pb: 2 }}>{dialogTitle}</DialogTitle>
          <DialogContent sx={{ typography: "body2", overflowWrap: "anywhere" }}>{message}</DialogContent>
          <DialogActions>
            <Button variant="text" color="inherit" sx={{ flex: { xs: 1, sm: "none" } }} onClick={() => setOpen(false)}>
              {cancelLabel}
            </Button>
            <Button variant="contained" color="error" sx={{ flex: { xs: 1, sm: "none" } }} onClick={confirm}>
              {confirmLabel}
            </Button>
          </DialogActions>
      </Dialog>
    </>
  );
}
