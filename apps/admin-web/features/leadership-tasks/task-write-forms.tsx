"use client";

import { useEffect, useId, useRef, useState } from "react";

import Box from "@mui/material/Box";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { visuallyHidden } from "@mui/utils";
import { varAlpha } from "minimal-shared/utils";

import { ThemedDatePicker } from "@/components/themed-date-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { splitFarmDeadlineLocal } from "./deadline";

/** Every hour of the farm's day, as the two-digit strings the form posts. */
const DEADLINE_HOURS = Array.from({ length: 24 }, (_, hour) => `${hour}`.padStart(2, "0"));
/** Five-minute steps; a stored deadline on an odd minute is added to the list so it round-trips. */
const DEADLINE_MINUTES = Array.from({ length: 12 }, (_, step) => `${step * 5}`.padStart(2, "0"));

/** The hour / minute listbox: the MUI TextField select every filter bar uses. */
const TIME_SELECT_PROPS = {
  inputLabel: { shrink: true },
  select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } },
} as const;

/**
 * The DEADLINE, in both modals: the console's own calendar for the day plus two selects for the
 * time, on the farm's clock (IST).
 *
 * It replaced a bare native `datetime-local`. Chrome draws that control's picker OUTSIDE the
 * modal box -- its hour and minute columns landed over the attachment buttons and Send -- and it
 * orders the day, month and year by the browser's locale rather than the DD/MM/YYYY every other
 * date in this console renders. `ThemedDatePicker` is the app's one date field; its popover opens
 * IN FLOW inside the modal (the sx below makes it static, as the phone filter sheet does for the
 * person popup), so it can neither be clipped by the modal body's scroller nor drawn over the
 * controls beneath it. The Server Action joins `deadline_date` + `deadline_hour` +
 * `deadline_minute` back into the `YYYY-MM-DDTHH:MM` shape it always read (`deadline.ts`).
 *
 * Hour and minute are MUI TextField selects NAMED `deadline_hour` / `deadline_minute`: the select
 * posts its value through its own hidden native input, by the name the Server Action reads. No
 * native `required`: the New task form checks day, hour and minute itself and says so in ONE
 * sentence (`missing`). The EDIT form passes `required=false`, where a blank day means "keep the
 * stored deadline".
 */
export function TaskDeadlineFields({
  pageContract,
  defaultLocal,
  min,
  required,
  missing = false,
  label,
  hint,
}: {
  pageContract?: AdminUiPageContract;
  /** `YYYY-MM-DDTHH:MM` on the farm's clock, or "" for nothing chosen yet. */
  defaultLocal: string;
  /** Earliest selectable day, `YYYY-MM-DD`. */
  min?: string;
  required: boolean;
  /**
   * The form found the day, hour or minute blank on Send. One sentence under the field says so
   * (2026-09-25): the browser's own bubble fired on the HOUR box with "Please select an item in
   * the list", and nothing told the raiser the DAY was missing.
   */
  missing?: boolean;
  label: string;
  hint: string;
}) {
  const initial = splitFarmDeadlineLocal(defaultLocal);
  const [hour, setHour] = useState(initial.hour);
  const [minute, setMinute] = useState(initial.minute);
  const wrapRef = useRef<HTMLDivElement>(null);
  const hintId = useId();
  const text = (key: string, fallback: string) =>
    pageContract ? copy(pageContract, key, fallback) : fallback;
  const minutes = DEADLINE_MINUTES.includes(minute) || !minute
    ? DEADLINE_MINUTES
    : [...DEADLINE_MINUTES, minute].sort();

  // The calendar opens in flow, so on a short phone it can land below the modal body's fold; it
  // is scrolled into view the moment it opens, the way the person popup is.
  useEffect(() => {
    const details = wrapRef.current?.querySelector("details");
    if (!details) return undefined;
    function onToggle() {
      if (details?.open) details.scrollIntoView({ block: "nearest" });
    }
    details.addEventListener("toggle", onToggle);
    return () => details.removeEventListener("toggle", onToggle);
  }, []);

  return (
    <Box
      className="lt-deadline"
      ref={wrapRef}
      data-required={required ? "true" : undefined}
      sx={{ display: "grid", gap: 1 }}
      // Escape unwinds ONE layer. The calendar and the modal shell both listen for Escape on
      // `document`; left alone, one press closed the calendar AND the modal, and the raiser lost
      // the whole form. Caught here first (React's capture phase runs at the root, before any
      // document listener), an open calendar swallows the press; the next press reaches the shell.
      onKeyDownCapture={(event) => {
        if (event.key !== "Escape") return;
        const details = wrapRef.current?.querySelector("details");
        if (!details?.open) return;
        event.preventDefault();
        event.nativeEvent.stopImmediatePropagation();
        details.open = false;
        (details.querySelector("summary") as HTMLElement | null)?.focus();
      }}
    >
      <Typography variant="subtitle2" component="span">{label}</Typography>
      <Box
        sx={{
          display: "grid",
          gap: 1,
          alignItems: "start",
          gridTemplateColumns: { xs: "1fr 1fr", sm: "minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr)" },
        }}
      >
        <Box
          className="lt-deadline-date"
          sx={(theme) => ({
            minWidth: 0,
            gridColumn: { xs: "1 / -1", sm: "auto" },
            // The console calendar's summary, drawn as the outlined TextField beside it (56px, the
            // template input radius) and its popover in flow (see the component comment).
            "& .move-date-button": {
              height: "var(--input-h)",
              px: 1.75,
              borderRadius: "var(--r-md)",
              borderColor: varAlpha(theme.vars.palette.grey["500Channel"], 0.2),
              bgcolor: "transparent",
              typography: "body1",
            },
            "& .move-date-popover": {
              position: "static",
              width: 1,
              mt: 1,
              boxShadow: "none",
              borderColor: theme.vars.palette.divider,
            },
          })}
        >
          <ThemedDatePicker
            name="deadline_date"
            label={text("deadline.day", "Choose a day")}
            min={min}
            // The New task form checks day, hour and minute itself and says so in ONE sentence
            // (`missing`); the picker's own required check opened the calendar with a second red
            // line ("Pick a day on or after ..."), so it is not asked to.
            required={false}
            defaultValue={initial.date}
            previousMonthLabel={text("date.previous_month", "Previous month")}
            nextMonthLabel={text("date.next_month", "Next month")}
            invalidDateText={text("deadline.day_min", "Pick a day on or after {date}.")}
          />
        </Box>
        <DeadlineTimeSelect
          name="deadline_hour"
          label={text("deadline.hour", "Hour")}
          value={hour}
          options={DEADLINE_HOURS}
          error={missing && !hour}
          describedBy={hintId}
          onChange={setHour}
        />
        <DeadlineTimeSelect
          name="deadline_minute"
          label={text("deadline.minute", "Minute")}
          value={minute}
          options={minutes}
          error={missing && !minute}
          describedBy={hintId}
          onChange={setMinute}
        />
      </Box>
      {missing ? (
        <Typography variant="caption" role="alert" data-testid="lt-deadline-missing" sx={{ color: "error.main" }}>
          {text("feedback.missing_deadline", "Choose the deadline day and time.")}
        </Typography>
      ) : null}
      {/* The deadline helper stays for screen readers (aria-describedby), not as a prose line. */}
      <Box component="span" id={hintId} sx={visuallyHidden}>
        {hint}
      </Box>
    </Box>
  );
}

/** One of the two time listboxes, NAMED as the Server Action reads it (the select posts its value
 *  through its own hidden native input). */
function DeadlineTimeSelect({
  name,
  label,
  value,
  options,
  error,
  describedBy,
  onChange,
}: {
  name: string;
  label: string;
  value: string;
  options: readonly string[];
  error: boolean;
  describedBy: string;
  onChange: (next: string) => void;
}) {
  return (
    <TextField
      select
      name={name}
      label={label}
      value={value}
      error={error}
      onChange={(event) => onChange(event.target.value)}
      fullWidth
      slotProps={{ ...TIME_SELECT_PROPS, select: { ...TIME_SELECT_PROPS.select, SelectDisplayProps: { "aria-describedby": describedBy } as never } }}
    >
      <MenuItem value="">--</MenuItem>
      {options.map((option) => (
        <MenuItem key={option} value={option}>
          {option}
        </MenuItem>
      ))}
    </TextField>
  );
}
