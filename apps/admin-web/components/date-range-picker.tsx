"use client";

import { CalendarDays, ChevronDown, ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

/**
 * A calendar that selects EITHER a single business day or an inclusive span.
 *
 * Presentational on purpose: it owns the popover, the month grid and the two-click range gesture,
 * and reports a chosen `(from, to)` upward. It does NOT touch the router — each host writes its own
 * parameters, which is what lets the Actions board (`vd_from`/`vd_to`, landing on today) and the
 * Weighing worklist (`wt_from`/`wt_to`, landing 30 days back) share one control instead of growing
 * two calendars that drift apart. There is precedent for that drift: six SQL paths once composed a
 * shed location six different ways (OL-7), which is why the shared helper rule exists.
 *
 * A single day is the span [d, d]. One shape, so a host never carries two date concepts.
 *
 * Future days are unpickable — the reads behind both hosts reject a future business date, and a
 * control that can express a request the server always refuses is a trap, not a feature.
 *
 * Every visible string arrives as a prop from the backend page contract (the backend-owns-labels
 * rule). Nothing here is a local literal except the date FORMAT, which is a locale concern.
 */

// Explicit, never the ambient locale: SSR and the hydrated client must format the same date to the
// same characters or React tears the tree down and rebuilds it. Same rule the top-bar picker
// carries, pinned by top-bar-date-picker-hydration.test.mjs.
const DATE_DISPLAY_LOCALE = "en-GB";

export type DateRangePickerLabels = {
  field: string;
  today: string;
  single: string;
  range: string;
  aria: string;
  previousMonth: string;
  nextMonth: string;
  rangeStartHint: string;
  rangeEndHint: string;
  rangeSeparator: string;
};

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

function formatShort(key: string): string {
  return new Intl.DateTimeFormat(DATE_DISPLAY_LOCALE, { day: "2-digit", month: "short", year: "numeric" }).format(
    parseDateKey(key),
  );
}

function formatFull(date: Date): string {
  return new Intl.DateTimeFormat(DATE_DISPLAY_LOCALE, { day: "numeric", month: "long", year: "numeric" }).format(date);
}

/** Interior of a span, both ends excluded. "YYYY-MM-DD" sorts lexicographically, so this is a
 *  plain string comparison — no Date object, no timezone re-entry. */
function strictlyBetween(key: string, from: string, to: string): boolean {
  return key.localeCompare(from) > 0 && key.localeCompare(to) < 0;
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

export function DateRangePicker({
  labels,
  from,
  to,
  today,
  busy = false,
  onChange,
}: {
  labels: DateRangePickerLabels;
  /** Inclusive selected span, both ends "YYYY-MM-DD". A single day is from === to. */
  from: string;
  to: string;
  /** Today's business day (Asia/Kolkata), resolved on the server. */
  today: string;
  /** True while the host's navigation is in flight; announced on the popover. */
  busy?: boolean;
  onChange: (from: string, to: string) => void;
}) {
  const detailsRef = useRef<HTMLDetailsElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);

  const [mode, setMode] = useState<"single" | "range">(() => (from === to ? "single" : "range"));
  // The first click of a two-click range selection. Null means "no range in progress".
  const [rangeStart, setRangeStart] = useState<string | null>(null);
  // Opens on the month of the LATER end, not the earlier one. A default window of "the 30 days
  // before today" starts in the previous month, so anchoring on `from` opened the grid on July
  // while the selection ran to 12 August — the reader could not see today, could not see where the
  // span ended, and had to page forward before picking anything. The recent end is also the one
  // being adjusted nearly every time.
  const [cursor, setCursor] = useState<Date>(() => {
    const anchor = parseDateKey(to);
    return new Date(anchor.getFullYear(), anchor.getMonth(), 1);
  });

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

  function commit(nextFrom: string, nextTo: string): void {
    setRangeStart(null);
    if (detailsRef.current) detailsRef.current.open = false;
    onChange(nextFrom, nextTo);
  }

  function pickDay(key: string): void {
    if (key > today) return;
    if (mode === "single") {
      commit(key, key);
      return;
    }
    if (!rangeStart) {
      setRangeStart(key);
      return;
    }
    if (key < rangeStart) commit(key, rangeStart);
    else commit(rangeStart, key);
  }

  function switchMode(next: "single" | "range"): void {
    setMode(next);
    setRangeStart(null);
    // Collapsing a span to one day needs a decision about WHICH day; the end of the range is the
    // most recent evidence, which is what a reader is nearly always after.
    if (next === "single" && from !== to) commit(to, to);
  }

  // Always the DATE, never the word "Today" (maintainer, 2026-08-12). The board is scoped to a
  // business day and the reader needs to know WHICH one off the chip; "Today" made them work it out,
  // and it read identically on a screenshot taken a week earlier. The calendar still marks today,
  // and the footer button still jumps back to it.
  const triggerValue =
    from === to ? formatShort(from) : `${formatShort(from)} ${labels.rangeSeparator} ${formatShort(to)}`;

  // While the first end of a range is chosen, the grid previews THAT day as the selection rather
  // than the range still on screen — otherwise the click appears to have done nothing.
  const previewFrom = rangeStart ?? from;
  const previewTo = rangeStart ?? to;

  return (
    <details
      ref={detailsRef}
      className="top-date-picker inline"
      // A filter row sits well down the page, so on a short window the calendar's last weeks and its
      // Today button open below the fold. `block: "nearest"` scrolls only as far as it has to, and
      // does nothing at all when the whole popover already fits — so a tall window never jumps.
      onToggle={(event) => {
        if (!event.currentTarget.open) return;
        popoverRef.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
      }}
    >
      <summary className="pscope date-scope" aria-label={labels.aria} data-testid="date-range-picker-trigger">
        <CalendarDays className="ic" aria-hidden="true" />
        <span className="date-scope-copy">
          <span className="date-scope-label">{labels.field}</span>
          <b className="date-scope-value">{triggerValue}</b>
        </span>
        <ChevronDown className="ic date-scope-chevron" aria-hidden="true" />
      </summary>
      <div ref={popoverRef} className="top-date-popover" role="group" aria-label={labels.aria} aria-busy={busy}>
        <div className="top-date-modes" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={mode === "single"}
            className={`top-date-mode${mode === "single" ? " on" : ""}`}
            onClick={() => switchMode("single")}
          >
            {labels.single}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "range"}
            className={`top-date-mode${mode === "range" ? " on" : ""}`}
            onClick={() => switchMode("range")}
          >
            {labels.range}
          </button>
        </div>

        <div className="top-date-head">
          <button
            type="button"
            className="top-date-arrow"
            aria-label={labels.previousMonth}
            onClick={() => setCursor((current) => addMonths(current, -1))}
          >
            <ChevronLeft className="ic" aria-hidden="true" />
          </button>
          <b>{formatMonth(cursor)}</b>
          <button
            type="button"
            className="top-date-arrow"
            aria-label={labels.nextMonth}
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
            const future = key > today;
            const isEdge = key === previewFrom || key === previewTo;
            const inRange = strictlyBetween(key, previewFrom, previewTo);
            return (
              <button
                key={key}
                type="button"
                disabled={future}
                className={[
                  "top-date-day",
                  sameMonth(day, cursor) ? "" : "outside",
                  key === today ? "today" : "",
                  isEdge ? "on" : "",
                  inRange ? "in-range" : "",
                ]
                  .filter(Boolean)
                  .join(" ")}
                aria-label={formatFull(day)}
                aria-pressed={isEdge}
                aria-current={key === today ? "date" : undefined}
                data-date={key}
                onClick={() => pickDay(key)}
              >
                {day.getDate()}
              </button>
            );
          })}
        </div>

        <div className="top-date-footer">
          {mode === "range" ? (
            <span className="top-date-hint">{rangeStart ? labels.rangeEndHint : labels.rangeStartHint}</span>
          ) : null}
          <button type="button" onClick={() => commit(today, today)}>
            {labels.today}
          </button>
        </div>
      </div>
    </details>
  );
}
