"use client";

import { useState } from "react";

import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import ListSubheader from "@mui/material/ListSubheader";

type SelectOption = { value: string; label: string; group?: string };

/**
 * MUI TextField select for this route's server-action filter <form>s. The value rides on a hidden input
 * carrying the field's `name`, so the form submits exactly what the native <select> submitted.
 *
 * Grouped options are supported: `SelectOption.group` renders a ListSubheader heading, the
 * <optgroup> equivalent, so the shed pickers keep park grouping without folding the park into each
 * shed's label (explicitly ruled out by the shed-filter comment in verification-review-page.tsx).
 */
export function VrFormSelect({
  id,
  name,
  label,
  options,
  defaultValue,
  minWidth = 180,
  className,
}: {
  /** Optional DOM id for the wrapper, so an existing anchor/label target keeps working. */
  id?: string;
  name: string;
  label: string;
  options: readonly SelectOption[];
  defaultValue?: string;
  minWidth?: number;
  className?: string;
}) {
  const [value, setValue] = useState(defaultValue ?? "");
  return (
    <div id={id} className={className}>
      <Box component="input" type="hidden" name={name} value={value} />
      <TextField
        select
        label={label}
        value={options.some((option) => option.value === value) ? value : ""}
        onChange={(event) => setValue(event.target.value)}
        sx={{ minWidth: { xs: 0, sm: minWidth }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {options.flatMap((option, index) => [
          ...(option.group && option.group !== options[index - 1]?.group ? [<ListSubheader key={`group-${index}`}>{option.group}</ListSubheader>] : []),
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>,
        ])}
      </TextField>
    </div>
  );
}
