"use client";

import { useState } from "react";

import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
const MINUTES = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, "0"));

/**
 * The kit's time field (replacing the native time input): hour and minute listboxes posting ONE text input under `name`
 * in the same `HH:MM` shape the native control produced, so the Server Action that reads it is
 * untouched. `required` rides on that input. Labels come from the caller.
 */
export function TimeField({
  name,
  hourLabel,
  minuteLabel,
  defaultValue,
  required,
  ariaLabel,
}: {
  name: string;
  hourLabel: string;
  minuteLabel: string;
  /** `HH:MM` or empty. */
  defaultValue?: string;
  required?: boolean;
  ariaLabel?: string;
}) {
  const [h0, m0] = (defaultValue ?? "").split(":");
  const [hour, setHour] = useState(h0 ?? "");
  const [minute, setMinute] = useState(m0 && MINUTES.includes(m0) ? m0 : m0 ? String(Math.round(Number(m0) / 5) * 5 % 60).padStart(2, "0") : "");
  const value = hour && minute ? `${hour}:${minute}` : "";
  return (
    <div className="kit-timefield" role="group" aria-label={ariaLabel}>
      <TextField
        select
        label={hourLabel}
        value={HOURS.includes(hour) ? hour : ""}
        onChange={(event) => setHour(event.target.value)}
        sx={{ flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">--</MenuItem>
        {HOURS.map((h) => (
          <MenuItem key={h} value={h}>
            {h}
          </MenuItem>
        ))}
      </TextField>
      <TextField
        select
        label={minuteLabel}
        value={minute}
        onChange={(event) => setMinute(event.target.value)}
        sx={{ flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">--</MenuItem>
        {MINUTES.map((m) => (
          <MenuItem key={m} value={m}>
            {m}
          </MenuItem>
        ))}
      </TextField>
      <input type="text" name={name} value={value} required={required} aria-hidden="true" tabIndex={-1} className="kit-posted-value" onChange={() => undefined} />
    </div>
  );
}
