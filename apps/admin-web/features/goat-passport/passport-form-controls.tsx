"use client";

import { useRef, useState } from "react";

import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

type SelectOption = { value: string; label: string };

/**
 * MUI TextField select wired for a server-action <form>: the chosen value rides on a hidden input
 * carrying the field's `name`, so the form submits exactly as the native <select> did. Purely a
 * presentation swap -- no change to the action, its payload, or validation.
 */
export function PassportFormSelect({
  name,
  label,
  options,
  required,
  defaultValue,
  emptyLabel,
}: {
  name: string;
  label: string;
  options: readonly SelectOption[];
  required?: boolean;
  defaultValue?: string;
  emptyLabel?: string;
}) {
  const [value, setValue] = useState(defaultValue ?? "");
  const all = emptyLabel ? [{ value: "", label: emptyLabel }, ...options] : [...options];
  return (
    <div className="fld">
      <input type="hidden" name={name} value={value} required={required} />
      <TextField
        select
        label={label}
        value={all.some((option) => option.value === value) ? value : ""}
        onChange={(event) => setValue(event.target.value)}
        sx={{ flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {all.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
    </div>
  );
}

/**
 * Submit button gated by a kit Dialog instead of window.confirm: the OS prompt cannot be themed
 * and blocks the page, and it is the one surface on this route that still looked like 2004.
 * Confirming submits the real form the button lives in, so the server action is untouched.
 */
export function PassportConfirmSubmitButton({
  children,
  message,
  confirmLabel,
  cancelLabel,
  className,
  disabled = false,
  title,
}: {
  children: React.ReactNode;
  message: string;
  confirmLabel: string;
  cancelLabel: string;
  className?: string;
  disabled?: boolean;
  title?: string;
}) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement | null>(null);
  return (
    <>
      <button
        ref={trigger}
        type="button"
        className={className}
        disabled={disabled}
        title={title}
        onClick={() => {
          if (!disabled) setOpen(true);
        }}
      >
        {children}
      </button>
      <Dialog fullWidth maxWidth="xs" open={open} onClose={() => setOpen(false)} slotProps={{ paper: { "aria-label": message } }}>
        <DialogContent sx={{ typography: "body2", pt: 3 }}>{message}</DialogContent>
        <DialogActions>
            <Button color="primary" variant="text" onClick={() => setOpen(false)}>
              {cancelLabel}
            </Button>
            <Button
              color="primary"
              variant="contained"
              onClick={() => {
                setOpen(false);
                trigger.current?.form?.requestSubmit();
              }}
            >
              {confirmLabel}
            </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
