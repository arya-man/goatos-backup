"use client";

import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

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

function addDays(date: Date, delta: number): Date {
  const next = new Date(date);
  next.setDate(date.getDate() + delta);
  return next;
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
  previousMonthLabel,
  nextMonthLabel,
  invalidFutureDateText,
  required,
}: {
  name: string;
  label: string;
  min?: string;
  previousMonthLabel: string;
  nextMonthLabel: string;
  invalidFutureDateText: string;
  required?: boolean;
}) {
  const minDate = useMemo(() => addDays(parseDateKey(min), 1), [min]);
  const minKey = dateKey(minDate);
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
      if (selected && selected >= minKey) {
        setError("");
        return;
      }
      event.preventDefault();
      setError(invalidFutureDateText.replace("{date}", minKey));
      if (detailsRef.current) detailsRef.current.open = true;
    }
    form.addEventListener("submit", onSubmit);
    return () => form.removeEventListener("submit", onSubmit);
  }, [invalidFutureDateText, minKey, required, selected]);

  function selectDate(key: string): void {
    setSelected(key);
    setError("");
    if (detailsRef.current) detailsRef.current.open = false;
  }

  return (
    <details ref={detailsRef} className="move-date-picker">
      <summary className="move-date-button">
        <span>{selected || label}</span>
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
          {["S", "M", "T", "W", "T", "F", "S"].map((day) => <span key={day} className="move-date-dow">{day}</span>)}
          {days.map((day) => {
            const key = dateKey(day);
            if (!sameMonth(day, cursor)) {
              return <span key={key} className="move-date-spacer" aria-hidden="true" />;
            }
            const disabled = key < minKey;
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
