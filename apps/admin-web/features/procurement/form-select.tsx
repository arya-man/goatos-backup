"use client";

import { useState } from "react";
import Box from "@mui/material/Box";

import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

import type { FormSelectOption } from "./option-utils";

// The template's visually-hidden recipe (components/minimal/table/table-head-custom), anchored
// under the field's start so a constraint-validation bubble points at the visible control.
const NATIVE_CARRIER: React.CSSProperties = {
  border: 0,
  margin: -1,
  padding: 0,
  width: 1,
  height: 1,
  overflow: "hidden",
  position: "absolute",
  whiteSpace: "nowrap",
  clip: "rect(0 0 0 0)",
  left: "var(--sp-1h)",
  bottom: 2,
  opacity: 0,
  pointerEvents: "none",
  appearance: "none",
};

/**
 * The MUI TextField select wired into a `<form>`.
 *
 * Every dropdown on Sales / Procurement used to be a native `<select>`, so each one rendered the
 * OS menu — a different typeface, a different highlight and no theme — in the middle of a themed
 * drawer. This swaps the *presentation* for the kit field and keeps the native control as the
 * form's actual value carrier, so `name`, `required` and the server action's payload are
 * untouched: the submitted value, its validation and the action that reads it all stay exactly
 * as they were.
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
  className,
  style,
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
  className?: string;
  style?: React.CSSProperties;
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
    // The native control stays a rendered 1px box (template `visuallyHidden`, as TableHeadCustom
    // uses for its sort label) so constraint validation can still focus it and anchor its bubble.
    // Inside a legacy `.fld` the outlined label notch needs clearance above the field.
    <Box
      // `pmx-fsel` stays as a hook for callers' own CSS (counts herd-actions modal).
      className={["pmx-fsel", className ?? ""].filter(Boolean).join(" ")}
      style={style}
      sx={{ position: "relative", display: "block", minWidth: 0, ".fld > &": { mt: 1 }, ".lw-actions .fld > &": { mt: 0 } }}
    >
      <TextField
        select
        label={label}
        value={options.some((option) => option.value === current) ? current : ""}
        disabled={disabled}
        title={title}
        onChange={(event) => commit(event.target.value)}
        sx={{ minWidth: { xs: 0, sm: minWidth ?? 0 }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {options.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
      {/* A field with no `name` is not submitted, but it IS still constraint-validated — which is
          how the shed picker (its value rides on hidden inputs) keeps its `required`. */}
      <select
        style={NATIVE_CARRIER}
        id={id}
        name={name}
        value={current}
        required={required}
        disabled={disabled}
        tabIndex={-1}
        onChange={(event) => commit(event.target.value)}
      >
        {options.map((option) => (
          <option key={option.value || "__empty"} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </Box>
  );
}
