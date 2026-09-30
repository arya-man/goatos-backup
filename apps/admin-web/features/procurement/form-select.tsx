"use client";

import { useState } from "react";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

import type { FormSelectOption } from "./option-utils";

/**
 * The MUI TextField select wired into a `<form>`.
 *
 * Every dropdown on Sales / Procurement used to be a native `<select>`, so each one rendered the
 * OS menu — a different typeface, a different highlight and no theme — in the middle of a themed
 * drawer. This is the template TextField select only: MUI's Select renders its own visually hidden
 * native input carrying `name`, `value` and `required`, so the server action's payload and the
 * browser's constraint validation (bubble anchored under the field) stay exactly as they were,
 * with no second native control (guard `legacy-free-zone`).
 */
export function FormSelect({
  label,
  name,
  options,
  defaultValue = "",
  value,
  onValueChange,
  required = false,
  disabled = false,
  id,
  title,
  minWidth,
  size,
  fullWidth = false,
}: {
  label: string;
  name?: string;
  options: readonly FormSelectOption[];
  defaultValue?: string;
  /** Controlled value; omit to let the field keep its own state like a `defaultValue` select. */
  value?: string;
  onValueChange?: (value: string) => void;
  required?: boolean;
  disabled?: boolean;
  id?: string;
  title?: string;
  minWidth?: number;
  size?: "small" | "medium";
  /** Template form fields fill their grid cell (product create/edit `Field.Select`). */
  fullWidth?: boolean;
}) {
  const [own, setOwn] = useState(defaultValue);
  // A caller that swaps its option list (park -> shed) can leave the held value with no matching
  // option. Fall back to the first option the way the native control did, DERIVED during render
  // rather than corrected in an effect, so there is no second pass and no stale frame.
  const ownInOptions = options.some((option) => option.value === own);
  const current = value ?? (ownInOptions ? own : (options[0]?.value ?? ""));

  function commit(next: string) {
    if (value === undefined) setOwn(next);
    onValueChange?.(next);
  }

  return (
    <TextField
      select
      id={id}
      name={name}
      required={required}
      label={label}
      size={size}
      fullWidth={fullWidth}
      value={options.some((option) => option.value === current) ? current : ""}
      disabled={disabled}
      title={title}
      onChange={(event) => commit(event.target.value)}
      sx={{ minWidth: { xs: 0, sm: minWidth ?? 0 }, flexShrink: 0, maxWidth: 1 }}
      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
    >
      {options.map((option) => (
        <MenuItem key={option.value || "__empty"} value={option.value}>
          {option.label}
        </MenuItem>
      ))}
    </TextField>
  );
}
