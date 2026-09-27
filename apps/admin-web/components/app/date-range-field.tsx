"use client";

import dayjs, { type Dayjs } from "dayjs";
import Stack from "@mui/material/Stack";
import { DatePicker } from "@mui/x-date-pickers/DatePicker";

export type DateRangeFieldProps = {
  label: string;
  /** "YYYY-MM-DD" or "" for an open end. */
  from: string;
  to: string;
  fromLabel: string;
  toLabel: string;
  onChange: (next: { from: string; to: string }) => void;
  /** Accessible name of the clear control; the control is only shown while something is picked. */
  clearLabel?: string;
  previousMonthLabel?: string;
  nextMonthLabel?: string;
  /** Name prefix for the hidden inputs (`${name}_from` / `${name}_to`) when the field sits in a form. */
  name?: string;
  minWidth?: number;
  className?: string;
};

/**
 * Filter-bar date range on the template's date anatomy (TR1-#22): two MUI X DatePickers side by
 * side -- the start / end fields the template's list toolbars use (order / invoice list), each an
 * outlined field with its label on the border and the trailing calendar icon, DD/MM/YYYY. Either end
 * may be empty (an open-ended span), which a list filter needs; the end cannot go before the start
 * and vice versa. Each field is clearable (MUI X field clear button). The start field carries the
 * range's own label (`fromLabel` / `clearLabel` stay in the props for callers; the template field
 * names the start by its label and the end "To"). `name` keeps the hidden form inputs
 * (`${name}_from` / `${name}_to`) for a field inside a form.
 */
export function DateRangeField({
  label,
  from,
  to,
  toLabel,
  onChange,
  previousMonthLabel = "Previous month",
  nextMonthLabel = "Next month",
  name = "dates",
  minWidth = 312,
  className,
}: DateRangeFieldProps) {
  const arrows = {
    previousIconButton: { "aria-label": previousMonthLabel } as never,
    nextIconButton: { "aria-label": nextMonthLabel } as never,
  };
  const key = (value: Dayjs | null): string | null => (value === null ? "" : value.isValid() ? value.format("YYYY-MM-DD") : null);
  const fieldSx = { flex: { sm: "1 1 0" }, width: { xs: 1, sm: "auto" }, minWidth: { xs: 0, sm: Math.max(160, Math.round(minWidth / 2) - 8) } };
  return (
    // Side by side from sm (MUI Stack row, template DatePicker sizing); stacked full width on phones.
    <Stack
      role="group"
      aria-label={label}
      className={className}
      direction={{ xs: "column", sm: "row" }}
      spacing={2}
      sx={{ minWidth: 0, maxWidth: 1, width: { xs: 1, sm: "auto" }, flex: { sm: "1 1 auto" } }}
    >
      <input type="hidden" name={`${name}_from`} value={from} />
      <input type="hidden" name={`${name}_to`} value={to} />
      <DatePicker
        label={label}
        value={from ? dayjs(from) : null}
        format="DD/MM/YYYY"
        maxDate={to ? dayjs(to) : undefined}
        onChange={(value) => {
          const next = key(value);
          if (next !== null && next !== from) onChange({ from: next, to });
        }}
        slotProps={{ ...arrows, field: { clearable: true } as never, textField: { sx: fieldSx } }}
      />
      <DatePicker
        label={toLabel}
        value={to ? dayjs(to) : null}
        format="DD/MM/YYYY"
        minDate={from ? dayjs(from) : undefined}
        onChange={(value) => {
          const next = key(value);
          if (next !== null && next !== to) onChange({ from, to: next });
        }}
        slotProps={{ ...arrows, field: { clearable: true } as never, textField: { sx: fieldSx } }}
      />
    </Stack>
  );
}
