"use client";

import { useState } from "react";

import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

type SelectOption = { value: string; label: string };

/**
 * MUI `TextField select` for a <form> field that a native <select name=…> used to carry.
 *
 * The kit select is a button + listbox, so it submits nothing on its own. The chosen value rides
 * on a hidden input carrying the field's `name`, which means the surrounding server action keeps
 * receiving exactly what the native control sent.
 *
 * Two modes, matching the two shapes the native controls had:
 *  - uncontrolled (`defaultValue`, no `value`) — the component owns the value, like `<select defaultValue>`;
 *  - controlled (`value` + `onChange`) — the page owns it, like `<select value onChange>`; used where
 *    one select filters another (park → shed).
 *
 * `required` is carried as `aria-required` only: a hidden input cannot be constraint-validated, and
 * a visually hidden focusable one makes Chrome report "not focusable" on submit. The server action
 * remains the validator, which it already was.
 */
export function FormSelect({
  name,
  label,
  options,
  defaultValue,
  value,
  onChange,
  disabled = false,
  required = false,
  minWidth = 160,
  className,
  title,
  helperText,
  fullWidth = false,
}: {
  /** Omit for a select that only drives other fields and submits nothing of its own. */
  name?: string;
  label: string;
  options: readonly SelectOption[];
  defaultValue?: string;
  value?: string;
  onChange?: (value: string) => void;
  disabled?: boolean;
  required?: boolean;
  minWidth?: number;
  className?: string;
  title?: string;
  /** The field's hint under it (MUI TextField helperText). */
  helperText?: React.ReactNode;
  /** Stretch to the container (dialog grid fields). */
  fullWidth?: boolean;
}) {
  const [own, setOwn] = useState(defaultValue ?? "");
  const controlled = value !== undefined;
  const current = controlled ? value : own;
  return (
    <div className={className} style={{ minWidth: 0 }} aria-required={required || undefined}>
      {name ? <input type="hidden" name={name} value={current} /> : null}
      <TextField
        select
        label={label}
        value={options.some((option) => option.value === current) ? current : ""}
        disabled={disabled}
        title={title}
        helperText={helperText}
        fullWidth={fullWidth}
        onChange={({ target: { value: next } }) => {
          if (!controlled) setOwn(next);
          onChange?.(next);
        }}
        sx={{ minWidth: { xs: 0, sm: minWidth }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {options.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
    </div>
  );
}
