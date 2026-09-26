"use client";

import { forwardRef, type FocusEventHandler } from "react";
import { X } from "lucide-react";
import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import type { InputBaseComponentProps } from "@mui/material/InputBase";

import { ThemedDatePicker } from "@/components/themed-date-picker";

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
 * Filter-bar date range built from two themed calendars — the kit's answer to a pair of native
 * native date inputs. Either end may be empty (an open-ended span), which is what a list filter
 * needs and what `DateRangePicker` (always a closed span with a resolved "today") does not offer.
 * The `to` calendar cannot go before `from` and vice versa.
 */
export function DateRangeField({
  label,
  from,
  to,
  fromLabel,
  toLabel,
  onChange,
  clearLabel = "Clear dates",
  previousMonthLabel = "Previous month",
  nextMonthLabel = "Next month",
  name = "dates",
  minWidth = 312,
  className,
}: DateRangeFieldProps) {
  const hasValue = Boolean(from || to);
  return (
    <TextField
      size="small"
      label={label}
      className={["kit-daterange", hasValue ? "has-value" : "", className ?? ""].filter(Boolean).join(" ")}
      sx={{ minWidth, "& .MuiInputBase-input": { px: 0 } }}
      slotProps={{
        inputLabel: { shrink: true },
        input: { inputComponent: DateRangeInputs },
        htmlInput: { range: { label, from, to, fromLabel, toLabel, onChange, clearLabel, previousMonthLabel, nextMonthLabel, name } satisfies RangeProps },
      }}
    />
  );
}

type RangeProps = Required<Omit<DateRangeFieldProps, "minWidth" | "className">>;

/**
 * The two calendars as the outlined input's `inputComponent` (MUI's documented slot for a custom
 * input inside TextField), so the label, notch, hover and focus outline all come from TextField.
 */
const DateRangeInputs = forwardRef<HTMLDivElement, InputBaseComponentProps>(function DateRangeInputs({ className, onFocus, onBlur, range }, ref) {
  const { label, from, to, fromLabel, toLabel, onChange, clearLabel, previousMonthLabel, nextMonthLabel, name } = range as RangeProps;
  const hasValue = Boolean(from || to);
  return (
    <div
      ref={ref}
      className={className}
      onFocus={onFocus as unknown as FocusEventHandler<HTMLDivElement>}
      onBlur={onBlur as unknown as FocusEventHandler<HTMLDivElement>}
    >
      <Box className="kit-daterange-control" role="group" aria-label={label} sx={{ display: "flex", alignItems: "center", height: 1 }}>
        <ThemedDatePicker
          name={`${name}_from`}
          label={fromLabel}
          value={from}
          max={to || undefined}
          onChange={(key) => onChange({ from: key, to })}
          previousMonthLabel={previousMonthLabel}
          nextMonthLabel={nextMonthLabel}
          invalidDateText=""
        />
        <span className="kit-daterange-sep" aria-hidden="true">→</span>
        <ThemedDatePicker
          name={`${name}_to`}
          label={toLabel}
          value={to}
          min={from || undefined}
          onChange={(key) => onChange({ from, to: key })}
          previousMonthLabel={previousMonthLabel}
          nextMonthLabel={nextMonthLabel}
          invalidDateText=""
        />
        {hasValue ? (
          <button type="button" className="kit-daterange-clear" aria-label={clearLabel} title={clearLabel} onClick={() => onChange({ from: "", to: "" })}>
            <X aria-hidden="true" />
          </button>
        ) : null}
      </Box>
    </div>
  );
});
