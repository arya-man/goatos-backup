"use client";

// The app's ONE date field.
//
// A native date input renders the browser's own control and the OS calendar popover: different
// chrome from every other field on the page, and a locale-driven day/month/year order that
// contradicts the DD-MM-YYYY rule the rest of the app renders through fmtDate. Two existing tests
// already ban the native input for exactly that reason -- and they ban it by scanning THIS file for
// the attribute, so do not name it here either. This is the component they expect instead.
//
// It moved here from features/preventive-care-vaccination on 2026-08-27, unchanged in behaviour
// but no longer min-only: it now takes an optional `max` so a field bounded in the OTHER direction
// (a sale date, which may be in the past but never in the future) can use the same control rather
// than fall back to a native input.
import { CalendarDays } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import dayjs from "dayjs";
import { DateCalendar } from "@mui/x-date-pickers/DateCalendar";

import { fmtDate } from "@/lib/format";
import dp from "./themed-date-picker.module.css";
import { DropdownPaper } from "@/components/app/dropdown-paper";

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
  /** Controlled use: what the button reads while nothing is picked, if not the `label`. */
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
  // Open on the month of the value being edited, else on the earliest allowed month (today when
  // unbounded), so a correction form does not make the operator page back to the original day.
  const [cursor, setCursor] = useState<Date>(() => parseDateKey(defaultValue || min));
  const detailsRef = useRef<HTMLDetailsElement>(null);

  useEffect(() => {
    function onPointerDown(event: PointerEvent): void {
      const details = detailsRef.current;
      if (!details?.open || !event.target || details.contains(event.target as Node)) return;
      details.open = false;
    }
    function onKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") return;
      const details = detailsRef.current;
      if (!details?.open) return;
      event.preventDefault();
      details.open = false;
    }
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  useEffect(() => {
    const form = detailsRef.current?.closest("form");
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
      if (detailsRef.current) detailsRef.current.open = true;
    }
    form.addEventListener("submit", onSubmit);
    return () => form.removeEventListener("submit", onSubmit);
  }, [invalidDateText, maxKey, minKey, required, selected]);

  function selectDate(key: string): void {
    setInternal(key);
    onChange?.(key);
    setError("");
    if (detailsRef.current) detailsRef.current.open = false;
  }

  return (
    <details ref={detailsRef} className={`move-date-picker ${dp.picker}`}>
      <summary className="move-date-button">
        {/* DD-MM-YYYY like every other visible date in the app; the ISO key stays on the hidden
            input, which is what the form actually submits. */}
        <span>{selected ? fmtDate(selected) : cleared ?? label}</span>
        <CalendarDays className="ic" aria-hidden="true" />
      </summary>
      <input type="hidden" name={name} value={selected} />
      <DropdownPaper className="move-date-popover" role="group" aria-label={label}>
        {/* Template calendar: MUI X DateCalendar (same component the CustomDateRangePicker uses).
            Hidden input above still carries the ISO YYYY-MM-DD value the form submits. Aria labels
            for the month arrows keep the backend-composed copy (`previousMonthLabel`, `nextMonthLabel`). */}
        <DateCalendar
          value={selected ? dayjs(selected) : null}
          referenceDate={dayjs(cursor)}
          onChange={(next) => {
            if (next) selectDate(next.format("YYYY-MM-DD"));
          }}
          onMonthChange={(month) => setCursor(new Date(month.year(), month.month(), 1))}
          minDate={min ? dayjs(min) : undefined}
          maxDate={max ? dayjs(max) : undefined}
          views={["day"]}
          showDaysOutsideCurrentMonth
          fixedWeekNumber={6}
          slotProps={{
            previousIconButton: { "aria-label": previousMonthLabel } as never,
            nextIconButton: { "aria-label": nextMonthLabel } as never,
          }}
          sx={{ width: 1, maxWidth: 1, height: "auto" }}
        />
      </DropdownPaper>
      {error ? <span className="move-date-error" role="alert">{error}</span> : null}
    </details>
  );
}
