"use client";

import { useEffect, useRef, useState } from "react";

import dayjs, { type Dayjs } from "dayjs";
import Box from "@mui/material/Box";
import { DatePicker } from "@mui/x-date-pickers/DatePicker";

import { fmtDate } from "@/lib/format";

/**
 * A form date field on the template's date anatomy: one MUI X DatePicker, outlined, its label
 * floating over the control like every TextField beside it (the template product form), DD/MM/YYYY
 * with the trailing calendar icon. The form posts the ISO `YYYY-MM-DD` key under `name` through a
 * hidden input, exactly what ThemedDatePicker posted, so server actions are untouched.
 *
 * J2 P1-6: the Register animal dialog drew its dates as a caption ABOVE a summary button whose
 * placeholder repeated that caption ("DOB" / "DOB") and sat lower than the fields beside it.
 * `required` shows the label asterisk and refuses the submit with `invalidDateText` (the picker's
 * own hidden input cannot be constraint-validated), like ThemedDatePicker did.
 * guard: form-date-field-template (components/app/form-date-field.test.mjs)
 */
export function FormDateField({
  name,
  label,
  defaultValue,
  min,
  max,
  required,
  invalidDateText,
  previousMonthLabel,
  nextMonthLabel,
}: {
  name: string;
  label: string;
  /** Initial ISO day (YYYY-MM-DD). */
  defaultValue?: string;
  min?: string;
  max?: string;
  required?: boolean;
  /** Backend refusal copy; "{date}" is replaced with the crossed bound. */
  invalidDateText: string;
  previousMonthLabel: string;
  nextMonthLabel: string;
}) {
  const [value, setValue] = useState<Dayjs | null>(defaultValue ? dayjs(defaultValue) : null);
  const [error, setError] = useState("");
  const anchorRef = useRef<HTMLInputElement>(null);
  const iso = value && value.isValid() ? value.format("YYYY-MM-DD") : "";

  useEffect(() => {
    const form = anchorRef.current?.closest("form");
    if (!form) return undefined;
    function onSubmit(event: SubmitEvent): void {
      const belowMin = Boolean(min && iso && iso < min);
      const aboveMax = Boolean(max && iso && iso > max);
      if ((iso || !required) && !belowMin && !aboveMax) {
        setError("");
        return;
      }
      event.preventDefault();
      setError(invalidDateText.replace("{date}", fmtDate(belowMin ? min : aboveMax ? max : "") || ""));
    }
    form.addEventListener("submit", onSubmit);
    return () => form.removeEventListener("submit", onSubmit);
  }, [invalidDateText, iso, max, min, required]);

  return (
    <Box sx={{ minWidth: 0 }}>
      <input ref={anchorRef} type="hidden" name={name} value={iso} />
      <DatePicker
        label={label}
        value={value}
        format="DD/MM/YYYY"
        minDate={min ? dayjs(min) : undefined}
        maxDate={max ? dayjs(max) : undefined}
        onChange={(next) => {
          setValue(next);
          setError("");
        }}
        slotProps={{
          previousIconButton: { "aria-label": previousMonthLabel } as never,
          nextIconButton: { "aria-label": nextMonthLabel } as never,
          textField: {
            fullWidth: true,
            error: Boolean(error),
            helperText: error || undefined,
            // The asterisk only: a `required` picker input would be browser-validated while hidden.
            slotProps: { inputLabel: { shrink: true, required } },
          } as never,
        }}
      />
    </Box>
  );
}
