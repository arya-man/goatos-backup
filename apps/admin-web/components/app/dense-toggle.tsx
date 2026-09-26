"use client";

import FormControlLabel from "@mui/material/FormControlLabel";
import Switch from "@mui/material/Switch";

export type DenseToggleProps = {
  checked: boolean;
  onChange: (dense: boolean) => void;
  label?: string;
  className?: string;
};

// The template table footer's Dense switch (TablePaginationCustom: FormControlLabel + Switch).
// Flipping is purely presentational: put `kit-dense` on the table wrapper and the row padding tightens.
export function DenseToggle({ checked, onChange, label = "Dense", className }: DenseToggleProps) {
  return (
    <FormControlLabel
      className={className}
      label={label}
      control={<Switch checked={checked} onChange={(event) => onChange(event.target.checked)} />}
      sx={{ m: 0 }}
    />
  );
}
