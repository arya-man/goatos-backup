"use client";

import { useState } from "react";

import { ThemedDatePicker } from "@/components/themed-date-picker";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
const MINUTES = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, "0"));

/**
 * The kit's answer to the native datetime-local input: the themed calendar for the day plus hour
 * and minute listboxes, posting ONE text input under `name` in the exact
 * `YYYY-MM-DDTHH:MM` shape the native control produced, so every Server Action that reads the
 * field (`optRfc3339` / `requiredRfc3339`) is untouched. `required` rides on that input, so a
 * form with a day but no time is still refused by the browser before it is sent.
 * Labels arrive from the caller (backend copy); this component renders none of its own.
 */
export function DateTimeField({
  name,
  label,
  required,
  defaultValue,
  hourLabel,
  minuteLabel,
  previousMonthLabel,
  nextMonthLabel,
  invalidDateText,
  min,
  max,
}: {
  name: string;
  label: string;
  required?: boolean;
  /** `YYYY-MM-DDTHH:MM` (what the native control stored), or empty. */
  defaultValue?: string;
  hourLabel: string;
  minuteLabel: string;
  previousMonthLabel: string;
  nextMonthLabel: string;
  invalidDateText: string;
  min?: string;
  max?: string;
}) {
  const [initialDate, initialTime] = (defaultValue ?? "").split("T");
  const [date, setDate] = useState(initialDate ?? "");
  const [hour, setHour] = useState(initialTime ? initialTime.slice(0, 2) : "");
  const [minute, setMinute] = useState(initialTime ? initialTime.slice(3, 5) : "");
  const value = date && hour && minute ? `${date}T${hour}:${minute}` : "";
  return (
    <div className="kit-datetime">
      <div className="kit-datetime-date">
        <ThemedDatePicker
          name={`${name}__day`}
          label={label}
          value={date}
          onChange={setDate}
          min={min}
          max={max}
          previousMonthLabel={previousMonthLabel}
          nextMonthLabel={nextMonthLabel}
          invalidDateText={invalidDateText}
        />
      </div>
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
        value={MINUTES.includes(minute) ? minute : ""}
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
