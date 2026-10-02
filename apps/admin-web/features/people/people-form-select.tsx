"use client";

import { useState } from "react";
import { flushSync } from "react-dom";

import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

type SelectOption = { value: string; label: string };

/**
 * MUI TextField select for the People screens' server-action <form>s. The chosen value rides on a
 * hidden input carrying the field's `name`, so submission, the action and its payload are exactly
 * what the native <select> produced -- this is a presentation swap only.
 */
export function PeopleFormSelect({
  name,
  label,
  options,
  defaultValue,
  required,
  disabled,
  minWidth = 180,
  form,
  autoSubmit,
}: {
  name: string;
  label: string;
  options: readonly SelectOption[];
  defaultValue?: string;
  required?: boolean;
  disabled?: boolean;
  minWidth?: number;
  /** Owning form id, so the value still submits when the field is rendered in a portal. */
  form?: string;
  /**
   * Submit the owning form as soon as a value is picked (template UserTableToolbar: filters apply
   * on change, no Apply button; TR-2 P1-4). Needs `form`.
   */
  autoSubmit?: boolean;
}) {
  const [value, setValue] = useState(defaultValue ?? "");
  const labelMinWidth = Math.min(260, Math.max(minWidth, label.length * 7 + 24));
  return (
    <Box sx={{ minWidth: labelMinWidth }}>
      <input type="hidden" name={name} value={value} required={required} form={form} />
      <TextField
        select
        label={label}
        value={options.some((option) => option.value === value) ? value : ""}
        disabled={disabled}
        onChange={(event) => {
          // flushSync: the hidden input carries the new value before the form is submitted.
          flushSync(() => setValue(event.target.value));
          if (autoSubmit && form) (document.getElementById(form) as HTMLFormElement | null)?.requestSubmit();
        }}
        // Fills its wrapper: on a phone the filter row stacks each wrapper full width, and a select
        // sized to its "All" value shrank to ~100px and read "Desig…" (PR #294 P6).
        sx={{ width: 1, minWidth: { xs: 0, sm: minWidth }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {options.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
    </Box>
  );
}
