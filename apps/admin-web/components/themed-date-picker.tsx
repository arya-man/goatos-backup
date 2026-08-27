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
import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

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

function monthTitle(date: Date): string {
  return new Intl.DateTimeFormat("en", { month: "long", year: "numeric" }).format(date);
}

function sameMonth(left: Date, right: Date): boolean {
  return left.getFullYear() === right.getFullYear() && left.getMonth() === right.getMonth();
}

function addMonths(date: Date, delta: number): Date {
  return new Date(date.getFullYear(), date.getMonth() + delta, 1);
}

function buildMonthDays(cursor: Date): Date[] {
  const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1);
  const start = new Date(first);
  start.setDate(first.getDate() - first.getDay());
  return Array.from({ length: 42 }, (_, index) => {
    const day = new Date(start);
    day.setDate(start.getDate() + index);
    return day;
  });
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
}) {
  const minDate = useMemo(() => parseDateKey(min), [min]);
  // parseDateKey falls back to TODAY for an absent value, so the bounds are read off the raw props
  // rather than off minDate -- otherwise a field with no `min` would silently disable every past
  // day, which is the entire range a sale date needs.
  const minKey = min ? dateKey(minDate) : "";
  const maxKey = max ?? "";
  const [selected, setSelected] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [cursor, setCursor] = useState<Date>(() => parseDateKey(min));
  const detailsRef = useRef<HTMLDetailsElement>(null);
  const days = useMemo(() => buildMonthDays(cursor), [cursor]);
  const today = dateKey(new Date());

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
    setSelected(key);
    setError("");
    if (detailsRef.current) detailsRef.current.open = false;
  }

  return (
    <details ref={detailsRef} className="move-date-picker">
      <summary className="move-date-button">
        {/* DD-MM-YYYY like every other visible date in the app; the ISO key stays on the hidden
            input, which is what the form actually submits. */}
        <span>{selected ? fmtDate(selected) : label}</span>
        <CalendarDays className="ic" aria-hidden="true" />
      </summary>
      <input type="hidden" name={name} value={selected} />
      <div className="move-date-popover" role="group" aria-label={label}>
        <div className="move-date-head">
          <button type="button" aria-label={previousMonthLabel} onClick={() => setCursor((current) => addMonths(current, -1))}>
            <ChevronLeft className="ic" aria-hidden="true" />
          </button>
          <b>{monthTitle(cursor)}</b>
          <button type="button" aria-label={nextMonthLabel} onClick={() => setCursor((current) => addMonths(current, 1))}>
            <ChevronRight className="ic" aria-hidden="true" />
          </button>
        </div>
        <div className="move-date-grid">
          {["S", "M", "T", "W", "T", "F", "S"].map((day, index) => (
            <span key={`dow-${index}`} className="move-date-dow">{day}</span>
          ))}
          {days.map((day) => {
            const key = dateKey(day);
            if (!sameMonth(day, cursor)) {
              return <span key={key} className="move-date-spacer" aria-hidden="true" />;
            }
            const disabled = (minKey !== "" && key < minKey) || (maxKey !== "" && key > maxKey);
            return (
              <button
                key={key}
                type="button"
                className={[
                  "move-date-day",
                  key === today ? "today" : "",
                  selected === key ? "on" : "",
                ].filter(Boolean).join(" ")}
                disabled={disabled}
                aria-pressed={selected === key}
                onClick={() => selectDate(key)}
              >
                {day.getDate()}
              </button>
            );
          })}
        </div>
      </div>
      {error ? <span className="move-date-error" role="alert">{error}</span> : null}
    </details>
  );
}
