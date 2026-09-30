"use client";

// The app's ONE date field for forms and filters.
//
// FIXJ4 (J1 P1-4): a thin adapter over the template date anatomy -- one MUI X DatePicker, outlined,
// floating label, DD/MM/YYYY with the trailing calendar icon (the same field FormDateField and
// WorklistFilters render). It used to be a hand-made <details> summary button + DateCalendar
// dropdown on legacy date-picker classes and a stylesheet module; both are gone. Public props are
// unchanged, so callers did not move:
// - uncontrolled (a form): `name` posts the ISO YYYY-MM-DD key through a hidden input;
//   `defaultValue` seeds it; `required` / `min` / `max` refuse the submit with `invalidDateText`.
// - controlled (the Tasks desk's Dates filter, not a form): `value` + `onChange`.
// Never a native date input (two tests ban it by scanning this file for the attribute).
import { useEffect, useMemo, useRef, useState } from "react";

import dayjs, { type Dayjs } from "dayjs";
import Box from "@mui/material/Box";
import { DatePicker } from "@mui/x-date-pickers/DatePicker";

import { fmtDate } from "@/lib/format";

function parseDateKey(value?: string): Date {
  const [year, month, day] = (value ?? "").split("-").map((part) => Number.parseInt(part, 10));
  if (!year || !month || !day) return new Date();
  return new Date(year, month - 1, day);
}

function dateKey(date: Date): string {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function ThemedDatePicker({
  name,
  label,
  min,
  max,
  previousMonthLabel,
  nextMonthLabel,
  invalidDateText,
  required,
  defaultValue,
  value,
  onChange,
  cleared,
}: {
  name: string;
  label: string;
  /** Earliest selectable day (YYYY-MM-DD). Omit to allow any past date. */
  min?: string;
  /** Latest selectable day (YYYY-MM-DD). Omit to allow any future date. */
  max?: string;
  previousMonthLabel: string;
  nextMonthLabel: string;
  /** Backend-owned refusal copy. "{date}" is replaced with the bound that was crossed. */
  invalidDateText: string;
  required?: boolean;
  /** Initial selection (YYYY-MM-DD) for a form editing an already-recorded date. */
  defaultValue?: string;
  /**
   * CONTROLLED use (the Tasks desk's Dates filter, which is not a form): the host owns the
   * selection and hears every pick through `onChange`. Omit both for the form (uncontrolled) use.
   */
  value?: string;
  onChange?: (key: string) => void;
  /** Controlled use: the field's placeholder while nothing is picked (e.g. "Any"). */
  cleared?: string;
}) {
  const minDate = useMemo(() => parseDateKey(min), [min]);
  // parseDateKey falls back to TODAY for an absent value, so the bounds are read off the raw props
  // rather than off minDate -- otherwise a field with no `min` would silently disable every past
  // day, which is the entire range a sale date needs.
  const minKey = min ? dateKey(minDate) : "";
  const maxKey = max ?? "";
  const [internal, setInternal] = useState<string>(defaultValue ?? "");
  const selected = value ?? internal;
  const [error, setError] = useState<string>("");
  const anchorRef = useRef<HTMLInputElement>(null);
  // The field's own value: a half-typed date stays on screen without reaching the host.
  const [draft, setDraft] = useState<Dayjs | null>(() => (selected ? dayjs(selected) : null));
  useEffect(() => {
    setDraft((current) => {
      const currentKey = current && current.isValid() ? current.format("YYYY-MM-DD") : "";
      if (currentKey === selected) return current;
      return selected ? dayjs(selected) : null;
    });
  }, [selected]);

  useEffect(() => {
    const form = anchorRef.current?.closest("form");
    if (!form || !required) return undefined;
    function onSubmit(event: SubmitEvent): void {
      const belowMin = minKey !== "" && selected < minKey;
      const aboveMax = maxKey !== "" && selected > maxKey;
      if (selected && !belowMin && !aboveMax) {
        setError("");
        return;
      }
      event.preventDefault();
      setError(invalidDateText.replace("{date}", fmtDate(belowMin ? minKey : maxKey) || ""));
    }
    form.addEventListener("submit", onSubmit);
    return () => form.removeEventListener("submit", onSubmit);
  }, [invalidDateText, maxKey, minKey, required, selected]);

  return (
    <Box sx={{ minWidth: 0 }}>
      {/* The ISO key is what the form submits; the field shows DD/MM/YYYY like every visible date. */}
      <input ref={anchorRef} type="hidden" name={name} value={selected} />
      <DatePicker
        label={label}
        value={draft}
        format="DD/MM/YYYY"
        referenceDate={dayjs(defaultValue || min || undefined)}
        minDate={min ? dayjs(min) : undefined}
        maxDate={max ? dayjs(max) : undefined}
        onChange={(next) => {
          setDraft(next);
          if (next && !next.isValid()) return;
          const key = next ? next.format("YYYY-MM-DD") : "";
          setInternal(key);
          onChange?.(key);
          setError("");
        }}
        slotProps={{
          previousIconButton: { "aria-label": previousMonthLabel } as never,
          nextIconButton: { "aria-label": nextMonthLabel } as never,
          field: { clearable: !required } as never,
          textField: {
            fullWidth: true,
            error: Boolean(error),
            helperText: error || undefined,
            placeholder: selected ? undefined : cleared,
            // The asterisk only: a `required` picker input would be browser-validated while hidden.
            slotProps: { inputLabel: { shrink: true, required } },
          } as never,
        }}
      />
    </Box>
  );
}
