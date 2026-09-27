"use client";

import { useEffect, useMemo, useState } from "react";
import { DATE_RANGE_PICKER_MIN } from "@/components/app/filter-field-widths";
import dayjs from "dayjs";
import { varAlpha } from "minimal-shared/utils";
import Badge from "@mui/material/Badge";
import Box from "@mui/material/Box";
import MuiButton from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import { DateCalendar } from "@mui/x-date-pickers/DateCalendar";
import { PickerDay, type PickerDayProps } from "@mui/x-date-pickers/PickerDay";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { Iconify } from "@/components/minimal/iconify";

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
  markerHint?: string;
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

function monthStartKey(date: Date): string {
  return dateKey(new Date(date.getFullYear(), date.getMonth(), 1));
}

function monthEndKey(date: Date): string {
  return dateKey(new Date(date.getFullYear(), date.getMonth() + 1, 0));
}

function sameDateKeys(left: readonly string[], right: readonly string[]): boolean {
  if (left.length !== right.length) return false;
  return left.every((value, index) => value === right[index]);
}

// The VISIBLE picker label is DD/MM/YYYY like every other date on the page (maintainer
// decision 2026-09-10). formatFull*/aria-label below deliberately keeps the spoken long form
// ("10 September 2026"): a screen reader announcing "ten slash oh nine slash twenty twenty
// six" is worse, and an aria-label is not visible text.
function formatShort(key: string): string {
  return new Intl.DateTimeFormat(DATE_DISPLAY_LOCALE, { day: "2-digit", month: "2-digit", year: "numeric" }).format(
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

export function DateRangePicker({
  labels,
  from,
  to,
  today,
  busy = false,
  markerDates = [],
  markerFetchPath,
  minDate,
  singleDayOnly = false,
  emptyLabel,
  onChange,
}: {
  labels: DateRangePickerLabels;
  /** Inclusive selected span, both ends "YYYY-MM-DD". A single day is from === to. */
  from: string;
  to: string;
  /** Today's business day (Asia/Kolkata), resolved on the server. */
  today: string;
  /**
   * For a host whose filter is OPTIONAL: shown on the trigger while no span is chosen (`from` is
   * blank), so "no date filter" never reads as today's date. Hosts that always carry a window omit
   * it and behave exactly as before.
   */
  emptyLabel?: string;
  /** True while the host's navigation is in flight; announced on the popover. */
  busy?: boolean;
  /** Business-day keys that should show a small marker inside the calendar grid. */
  markerDates?: readonly string[];
  /**
   * Optional same-origin endpoint that accepts `from`/`to` business-day query params and returns
   * `{ dates: string[] }` for the visible calendar month. This is deliberately separate from the
   * selected report range: a reader opening August should see every August marker, even when the
   * dashboard is currently reporting 15 Aug -> 25 Aug.
   */
  markerFetchPath?: string;
  /**
   * Earliest selectable business day, inclusive, "YYYY-MM-DD". Days before it render
   * disabled exactly like future days do — for a host whose read has a hard history
   * floor (Herd Analytics starts 2026-08-01), a request the server would answer with
   * empty padding is as much of a trap as one it refuses.
   */
  minDate?: string;
  /**
   * Hides the single/range tabs and pins the calendar to ONE day.
   *
   * For a host that genuinely cannot express a span — the Video Log answers "what arrived on this
   * day", so a range would make every arrival time ambiguous about which day it belongs to. Without
   * this the tabs were still offered and a picked range silently collapsed to its start, which is a
   * control that does not do what it says.
   */
  singleDayOnly?: boolean;
  onChange: (from: string, to: string) => void;
}) {
  // Template CustomPopover anchored to the outlined trigger (portaled: no card can clip it).
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const open = Boolean(anchor);

  const [mode, setMode] = useState<"single" | "range">(() => (singleDayOnly || from === to ? "single" : "range"));
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

  const [fetchedMarkerDates, setFetchedMarkerDates] = useState<readonly string[]>([]);
  const markerMonthStartsInFuture = markerFetchPath ? monthStartKey(cursor) > today : false;
  const visibleMarkerDates = markerFetchPath ? (markerMonthStartsInFuture ? [] : fetchedMarkerDates) : markerDates;
  const markerDateSet = useMemo(() => new Set(visibleMarkerDates), [visibleMarkerDates]);

  useEffect(() => {
    if (!markerFetchPath || markerMonthStartsInFuture) return;
    const fromKey = monthStartKey(cursor);
    const endKey = monthEndKey(cursor);
    const toKey = endKey > today ? today : endKey;
    const controller = new AbortController();
    const url = new URL(markerFetchPath, window.location.origin);
    url.searchParams.set("from", fromKey);
    url.searchParams.set("to", toKey);
    fetch(url, { cache: "no-store", signal: controller.signal })
      .then((response) => (response.ok ? response.json() : { dates: [] }))
      .then((body: { dates?: string[] }) => {
        if (!controller.signal.aborted) {
          const nextDates = Array.isArray(body.dates) ? body.dates : [];
          setFetchedMarkerDates((current) => (sameDateKeys(current, nextDates) ? current : nextDates));
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setFetchedMarkerDates((current) => (current.length === 0 ? current : []));
      });
    return () => controller.abort();
  }, [cursor, markerFetchPath, markerMonthStartsInFuture, today]);


  function commit(nextFrom: string, nextTo: string): void {
    setRangeStart(null);
    setAnchor(null);
    onChange(nextFrom, nextTo);
  }

  function pickDay(key: string): void {
    if (key > today) return;
    if (minDate && key < minDate) return;
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
    !from && emptyLabel
      ? emptyLabel
      : from === to
        ? formatShort(from)
        : `${formatShort(from)} ${labels.rangeSeparator} ${formatShort(to)}`;

  // While the first end of a range is chosen, the grid previews THAT day as the selection rather
  // than the range still on screen — otherwise the click appears to have done nothing.
  const previewFrom = rangeStart ?? from;
  const previewTo = rangeStart ?? to;

  // Template anatomy: an outlined field trigger (the invoice toolbar's date inputs) opening a
  // CustomPopover with the MUI X DateCalendar the template's CustomDateRangePicker uses. The day slot
  // draws the span band, the ends and the host's markers; selection logic above is unchanged.
  const Day = (props: PickerDayProps) => {
    const key = props.day.format("YYYY-MM-DD");
    const future = key > today;
    const beforeFloor = minDate ? key < minDate : false;
    const isEdge = key === previewFrom || key === previewTo;
    const inRange = strictlyBetween(key, previewFrom, previewTo);
    const marked = markerDateSet.has(key);
    const label = marked && labels.markerHint ? `${formatFull(props.day.toDate())}. ${labels.markerHint}` : formatFull(props.day.toDate());
    return (
      <Badge
        overlap="circular"
        variant="dot"
        color="info"
        invisible={!marked || props.outsideCurrentMonth}
        title={marked ? labels.markerHint : undefined}
        slotProps={{ badge: { "aria-hidden": true } as never }}
      >
        <PickerDay
          {...props}
          disabled={future || beforeFloor}
          selected={isEdge && !props.outsideCurrentMonth}
          aria-label={label}
          aria-pressed={isEdge}
          data-date={key}
          sx={(theme) => (inRange && !props.outsideCurrentMonth ? { borderRadius: 0, bgcolor: varAlpha(theme.vars.palette.primary.mainChannel, 0.08) } : {})}
        />
      </Badge>
    );
  };

  return (
    <>
      <TextField
        label={labels.field}
        value={triggerValue}
        onClick={(event) => setAnchor(event.currentTarget)}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " " || event.key === "ArrowDown") {
            event.preventDefault();
            setAnchor(event.currentTarget);
          }
        }}
        data-testid="date-range-picker-trigger"
        sx={{ minWidth: { xs: 1, sm: DATE_RANGE_PICKER_MIN }, cursor: "pointer", "& *": { cursor: "pointer" } }}
        slotProps={{
          inputLabel: { shrink: true },
          htmlInput: { readOnly: true, "aria-label": labels.aria, "aria-haspopup": "dialog", "aria-expanded": open },
          input: {
            startAdornment: (
              <InputAdornment position="start">
                <Iconify icon="solar:calendar-date-bold" sx={{ color: "text.disabled" }} />
              </InputAdornment>
            ),
            endAdornment: (
              <InputAdornment position="end">
                <Iconify icon="eva:arrow-ios-downward-fill" width={18} sx={{ color: "text.secondary" }} />
              </InputAdornment>
            ),
          },
        }}
      />
      <CustomPopover open={open} anchorEl={anchor} onClose={() => { setRangeStart(null); setAnchor(null); }} slotProps={{ arrow: { placement: "top-left" } }}>
        <Box role="group" aria-label={labels.aria} aria-busy={busy} sx={{ p: 1, maxWidth: "calc(100vw - 32px)" }}>
          {singleDayOnly ? null : (
            <Tabs
              value={mode}
              onChange={(_event, next: "single" | "range") => switchMode(next)}
              indicatorColor="custom"
              variant="fullWidth"
              sx={{ mb: 1 }}
            >
              <Tab value="single" label={labels.single} />
              <Tab value="range" label={labels.range} />
            </Tabs>
          )}
          <DateCalendar
            value={dayjs(previewTo)}
            referenceDate={dayjs(cursor)}
            onChange={(next) => {
              if (next) pickDay(next.format("YYYY-MM-DD"));
            }}
            onMonthChange={(month) => setCursor(new Date(month.year(), month.month(), 1))}
            maxDate={dayjs(today)}
            minDate={minDate ? dayjs(minDate) : undefined}
            showDaysOutsideCurrentMonth
            fixedWeekNumber={6}
            views={["day"]}
            slots={{ day: Day }}
            slotProps={{ previousIconButton: { "aria-label": labels.previousMonth } as never, nextIconButton: { "aria-label": labels.nextMonth } as never }}
            sx={{ width: 1, maxWidth: 1, height: "auto" }}
          />
          <Box sx={{ px: 1, pb: 0.5, display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
            {mode === "range" ? (
              <Box component="span" sx={{ typography: "caption", color: "text.secondary", flex: "1 1 auto" }}>
                {rangeStart ? labels.rangeEndHint : labels.rangeStartHint}
              </Box>
            ) : (
              <Box sx={{ flex: "1 1 auto" }} />
            )}
            {labels.markerHint && markerDateSet.size > 0 ? (
              <Tooltip title={labels.markerHint}>
                <IconButton size="small" aria-label={labels.markerHint}>
                  <Iconify icon="eva:info-outline" width={18} />
                </IconButton>
              </Tooltip>
            ) : null}
            <MuiButton size="small" variant="outlined" color="inherit" onClick={() => commit(today, today)}>
              {labels.today}
            </MuiButton>
          </Box>
        </Box>
      </CustomPopover>
    </>
  );
}
