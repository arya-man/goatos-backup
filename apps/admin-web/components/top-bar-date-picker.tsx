"use client";

import { CalendarDays, ChevronDown, ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

const DATE_DISPLAY_LOCALE = "en-GB";

function parseDateKey(value: string): Date {
  const [year, month, day] = value.split("-").map((part) => Number.parseInt(part, 10));
  if (!year || !month || !day) return new Date();
  return new Date(year, month - 1, day);
}

function dateKey(date: Date): string {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day}`;
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

function formatMonth(date: Date): string {
  return new Intl.DateTimeFormat(DATE_DISPLAY_LOCALE, { month: "long", year: "numeric" }).format(date);
}

function formatSelectedDate(date: Date): string {
  return new Intl.DateTimeFormat(DATE_DISPLAY_LOCALE, { day: "2-digit", month: "short", year: "numeric" }).format(date);
}

function formatFullDate(date: Date): string {
  return new Intl.DateTimeFormat(DATE_DISPLAY_LOCALE, { day: "numeric", month: "long", year: "numeric" }).format(date);
}

function weekdayLabels(): string[] {
  const sunday = new Date(2026, 7, 2);
  const formatter = new Intl.DateTimeFormat(DATE_DISPLAY_LOCALE, { weekday: "narrow" });
  return Array.from({ length: 7 }, (_, index) => {
    const day = new Date(sunday);
    day.setDate(sunday.getDate() + index);
    return formatter.format(day);
  });
}

export function TopBarDatePicker({
  label,
  hint,
  selectedDate,
  todayDate,
  enabled,
  calendarAriaLabel,
  previousMonthLabel,
  nextMonthLabel,
  todayLabel,
  onSelectDate,
}: {
  label: string;
  hint: string;
  selectedDate: string;
  todayDate: string;
  enabled: boolean;
  calendarAriaLabel: string;
  previousMonthLabel: string;
  nextMonthLabel: string;
  todayLabel: string;
  onSelectDate: (date: string) => void;
}) {
  const selected = useMemo(() => parseDateKey(selectedDate), [selectedDate]);
  const [cursor, setCursor] = useState<Date>(() => new Date(selected.getFullYear(), selected.getMonth(), 1));
  const detailsRef = useRef<HTMLDetailsElement>(null);
  const days = useMemo(() => buildMonthDays(cursor), [cursor]);
  const weekdays = useMemo(() => weekdayLabels(), []);

  useEffect(() => {
    function onPointerDown(event: PointerEvent): void {
      const details = detailsRef.current;
      if (!details?.open || !event.target || details.contains(event.target as Node)) return;
      details.open = false;
    }

    function onKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape" || !detailsRef.current?.open) return;
      event.preventDefault();
      detailsRef.current.open = false;
    }

    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  function selectDate(value: string): void {
    if (!enabled) return;
    if (detailsRef.current) detailsRef.current.open = false;
    onSelectDate(value);
  }

  return (
    <details ref={detailsRef} className="top-date-picker">
      <summary
        className="pscope date-scope"
        title={hint}
        aria-label={calendarAriaLabel}
        aria-disabled={!enabled}
        data-testid="top-date-picker-trigger"
        onClick={(event) => {
          if (!enabled) event.preventDefault();
        }}
      >
        <CalendarDays className="ic" aria-hidden="true" />
        <span className="date-scope-copy">
          <span className="date-scope-label">{label}</span>
          <b className="date-scope-value">{formatSelectedDate(selected)}</b>
        </span>
        <ChevronDown className="ic date-scope-chevron" aria-hidden="true" />
      </summary>
      <div className="top-date-popover" role="group" aria-label={calendarAriaLabel}>
        <div className="top-date-head">
          <button
            type="button"
            className="top-date-arrow"
            aria-label={previousMonthLabel}
            onClick={() => setCursor((current) => addMonths(current, -1))}
          >
            <ChevronLeft className="ic" aria-hidden="true" />
          </button>
          <b>{formatMonth(cursor)}</b>
          <button
            type="button"
            className="top-date-arrow"
            aria-label={nextMonthLabel}
            onClick={() => setCursor((current) => addMonths(current, 1))}
          >
            <ChevronRight className="ic" aria-hidden="true" />
          </button>
        </div>
        <div className="top-date-grid">
          {weekdays.map((weekday, index) => (
            <span key={`${weekday}-${index}`} className="top-date-weekday" aria-hidden="true">
              {weekday}
            </span>
          ))}
          {days.map((day) => {
            const key = dateKey(day);
            return (
              <button
                key={key}
                type="button"
                className={[
                  "top-date-day",
                  sameMonth(day, cursor) ? "" : "outside",
                  key === todayDate ? "today" : "",
                  key === selectedDate ? "on" : "",
                ].filter(Boolean).join(" ")}
                aria-label={formatFullDate(day)}
                aria-pressed={key === selectedDate}
                aria-current={key === todayDate ? "date" : undefined}
                data-date={key}
                onClick={() => selectDate(key)}
              >
                {day.getDate()}
              </button>
            );
          })}
        </div>
        <div className="top-date-footer">
          <button type="button" onClick={() => selectDate(todayDate)}>{todayLabel}</button>
        </div>
      </div>
    </details>
  );
}
