"use client";

import { startTransition } from "react";
import { useRouter } from "next/navigation";

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
}: {
  label: string;
  value: string;
  options: readonly LinkSelectOption[];
  minWidth?: number;
}) {
  const router = useRouter();
  return (
    <TextField
      select
      label={label}
      value={options.some((option) => option.value === value) ? value : ""}
      onChange={({ target: { value: next } }) => {
        const target = options.find((option) => option.value === next);
        if (!target) return;
        startTransition(() => router.push(target.href, { scroll: false }));
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
