"use client";

import { useUrlNavigate } from "@/components/app/use-url-tab-nav";

import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

export type LinkSelectOption = { value: string; label: string; href: string };

/**
 * An MUI TextField select whose options are server-built links: choosing one navigates to that option's href
 * (scroll kept), exactly what the previous row of link chips did.
 */
export function LinkSelect({
  label,
  value,
  options,
  minWidth = 160,
  fullWidth = false,
}: {
  label: string;
  value: string;
  options: readonly LinkSelectOption[];
  minWidth?: number;
  /** Fill the parent (a toolbar slot that is full width on a phone). */
  fullWidth?: boolean;
}) {
  const { go } = useUrlNavigate();
  return (
    <TextField
      select
      fullWidth={fullWidth}
      label={label}
      value={options.some((option) => option.value === value) ? value : ""}
      onChange={({ target: { value: next } }) => {
        const target = options.find((option) => option.value === next);
        if (!target) return;
        go(target.href);
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
  );
}
