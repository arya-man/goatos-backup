"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import FullCalendar from "@fullcalendar/react";
import listPlugin from "@fullcalendar/list";
import dayGridPlugin from "@fullcalendar/daygrid";
import timeGridPlugin from "@fullcalendar/timegrid";
import type { EventClickArg, EventInput } from "@fullcalendar/core";

import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import useMediaQuery from "@mui/material/useMediaQuery";
import type { Theme } from "@mui/material/styles";

import { useRouter, useSearchParams } from "next/navigation";

import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { BlockSkeleton } from "@/components/app/skeletons";

/**
 * Params that never change the grid's events: the event drawer + its target pager, the action
 * feedback banner, and `as_of` (prev / next / today move the FullCalendar grid itself, at once).
 */
const GRID_IGNORE = ["event", "targets_cursor", "targets_page", "targets_cursor_stack", "action_status", "action_key", "as_of"] as const;

import { CalendarRoot, CalendarToolbar, type CalendarView } from "@/components/minimal/calendar";
import { pushLocalOverlayUrl } from "@/components/local-overlay-link";
import { scopeHref, type Scope } from "@/lib/scope";
import { fmtDate } from "@/lib/format";

export type CalendarViewOption = {
  value: CalendarView;
  label: string;
  icon: "mingcute:calendar-month-line" | "mingcute:calendar-week-line" | "mingcute:calendar-day-line" | "custom:calendar-agenda-outline";
};

// CalendarFullView — client shell that hosts FullCalendar on the template's CalendarRoot styles.
// Read-only (no editable/droppable/selectable/drag/resize); navigation is URL-driven via `asOf` so the
// server can fetch the right window; view toggle is client-local.
//
// Event click routing:
//   drive rows (extendedProps.isDrive) → router.push /calendar/drive/[id] (scope preserved by caller)
//   other rows                         → pushLocalOverlayUrl(#calendar_event=<id>) so CalendarEventDrawer opens locally
export function CalendarFullView({
  events,
  initialAsOf,
  scope,
  ownerKey,
  status,
  viewOptions,
  noEventsText,
  toolbarCopy,
  header,
}: {
  /** First row inside the card (the workstream Tabs, as the template list's status Tabs). */
  header?: React.ReactNode;
  events: EventInput[];
  initialAsOf: string;
  // Scope + owner/status ride in as plain data so the client can build hrefs itself.
  // (Server components in Next 16 cannot pass functions to client components without a
  // "use server" boundary; keeping the URL builder client-local is the simpler contract.)
  scope: Scope;
  ownerKey?: string;
  status?: string;
  // Toolbar view options; labels are backend-composed so the toolbar carries no local literals.
  viewOptions: CalendarViewOption[];
  // Empty-state copy for the FullCalendar list view.
  noEventsText: string;
  // Toolbar copy from the caller (see calendar.tsx server component).
  toolbarCopy: {
    today: string;
    previous: string;
    next: string;
    viewGroupAria: string;
  };
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const gridParams = Object.fromEntries(searchParams?.entries() ?? []);
  const calendarRef = useRef<FullCalendar | null>(null);
  const smUp = useMediaQuery<Theme>((theme) => theme.breakpoints.up("sm"));
  const mdUp = useMediaQuery<Theme>((theme) => theme.breakpoints.up("md"));

  // Template default: dayGridMonth on desktop, listWeek on phone (agenda-style list matches the
  // template's mobile calendar and gives a phone-thumb-friendly, no-sideways-scroll surface).
  const [view, setView] = useState<CalendarView>(smUp ? "dayGridMonth" : "listWeek");
  const [title, setTitle] = useState<string>("");

  // Keep the view in sync with viewport transitions (desktop <-> phone) exactly like the template's
  // useCalendar hook does. Only fires when the calendar API is mounted.
  useEffect(() => {
    const api = calendarRef.current?.getApi();
    if (!api) return;
    const target: CalendarView = smUp ? "dayGridMonth" : "listWeek";
    if (target !== api.view.type) {
      api.changeView(target);
      setView(target);
    }
    setTitle(viewTitle(api.view));
  }, [smUp]);

  const onChangeView = useCallback((next: CalendarView) => {
    const api = calendarRef.current?.getApi();
    if (!api) return;
    api.changeView(next);
    setView(next);
    setTitle(viewTitle(api.view));
  }, []);

  const calendarBase = useCallback(
    (extra: Record<string, string | undefined> = {}) =>
      scopeHref("/calendar", scope, {}, {
        owner_key: ownerKey && ownerKey !== "all" ? ownerKey : undefined,
        status,
        ...extra,
      }),
    [ownerKey, scope, status],
  );

  const onDateNavigation = useCallback(
    (action: "today" | "prev" | "next") => {
      const api = calendarRef.current?.getApi();
      if (!api) return;
      if (action === "today") api.today();
      if (action === "prev") api.prev();
      if (action === "next") api.next();
      setTitle(viewTitle(api.view));
      const nextAnchor = toIsoDate(api.view.currentStart);
      // Server-driven range: push the new anchor date to URL so the /calendar server tree
      // re-fetches a wider window around it. Uses replace to avoid stacking history entries.
      router.replace(calendarBase({ as_of: nextAnchor }), { scroll: false });
    },
    [calendarBase, router],
  );

  const onClickEvent = useCallback(
    (arg: EventClickArg) => {
      arg.jsEvent.preventDefault();
      const props = arg.event.extendedProps as { isDrive?: boolean };
      const id = arg.event.id;
      if (props.isDrive) {
        router.push(scopeHref(`/calendar/drive/${encodeURIComponent(id)}`, scope), { scroll: false });
        return;
      }
      const fragmentHref = `${calendarBase()}#calendar_event=${encodeURIComponent(id)}`;
      // pushLocalOverlayUrl handles the guard that CalendarEventDrawer listens for and dispatches
      // the URL-change event; falls back to full navigation if the guard refuses.
      if (!pushLocalOverlayUrl(fragmentHref)) {
        router.push(fragmentHref, { scroll: false });
      }
    },
    [calendarBase, router, scope],
  );

  return (
    <Card
      sx={{
        display: "flex",
        flexDirection: "column",
        flex: "1 1 auto",
        // Phone: the agenda list gets a tall card. md+: FullCalendar is height="auto", so the month
        // grid is laid out at its natural height and no internal scroller clips the last week row.
        minHeight: { xs: "70vh", md: 0 },
      }}
    >
      {header}
      <CalendarRoot sx={{ display: "flex", flexDirection: "column", flex: "1 1 auto", minHeight: 0 }}>
        <CalendarToolbar
          view={view}
          title={title}
          onChangeView={onChangeView}
          onDateNavigation={onDateNavigation}
          viewOptions={viewOptions}
          todayLabel={toolbarCopy.today}
          previousLabel={toolbarCopy.previous}
          nextLabel={toolbarCopy.next}
          viewGroupAriaLabel={toolbarCopy.viewGroupAria}
        />

        {/* The events grid (guard: url-keyed-panel): an owner / workstream / week-history click swaps it
            to its skeleton at once; the workstream strip and the toolbar stay on screen. */}
        <UrlSuspense searchParams={gridParams} watch={[ALL_PARAMS]} ignore={GRID_IGNORE} fallback={<BlockSkeleton card={false} height={{ xs: "60vh", md: "calc(100dvh - 320px)" }} />}>
        <Box sx={{ flex: "1 1 auto", display: "flex", flexDirection: "column", "& .fc": { flex: "1 1 auto" } }}>
          <FullCalendar
            ref={calendarRef}
            firstDay={1}
            weekends
            aspectRatio={3}
            height={mdUp ? "auto" : undefined}
            stickyHeaderDates={false}
            views={VIEW_DATE_FORMATS}
            dayMaxEvents={3}
            eventMaxStack={2}
            rerenderDelay={10}
            headerToolbar={false}
            eventDisplay="block"
            initialView={view}
            initialDate={initialAsOf}
            events={events}
            editable={false}
            droppable={false}
            selectable={false}
            eventClick={onClickEvent}
            datesSet={(arg) => setTitle(viewTitle(arg.view))}
            plugins={[dayGridPlugin, timeGridPlugin, listPlugin]}
            businessHours={{ daysOfWeek: [1, 2, 3, 4, 5] }}
            noEventsText={noEventsText}
          />
        </Box>
        </UrlSuspense>
      </CalendarRoot>
    </Card>
  );
}

/**
 * Toolbar title. A month view keeps FullCalendar's month heading ("September 2026" has no day, so
 * DD/MM/YYYY does not apply); week / day / agenda titles are DD/MM/YYYY via lib/format.
 */
function viewTitle(view: { type: string; title: string; currentStart: Date; currentEnd: Date }): string {
  if (view.type === "dayGridMonth") return view.title;
  const start = fmtDate(toIsoDate(view.currentStart));
  const last = new Date(view.currentEnd.getTime());
  last.setDate(last.getDate() - 1);
  const end = fmtDate(toIsoDate(last));
  return start === end ? start : `${start} – ${end}`;
}

type FcDateParts = { year: number; month: number; day: number };

// Module-level (stable identity): a fresh `views` object each render makes FullCalendar re-apply its
// options, fire datesSet, set the title and render again, forever.
const VIEW_DATE_FORMATS = {
  timeGridWeek: { dayHeaderFormat: (arg: { date: FcDateParts }) => `${weekdayOf(arg.date)} ${ddmmyyyy(arg.date)}` },
  listWeek: { listDaySideFormat: (arg: { date: FcDateParts }) => ddmmyyyy(arg.date) },
};

/** FullCalendar date parts (month is 0-based) as DD/MM/YYYY through lib/format. */
function ddmmyyyy(date: FcDateParts): string {
  return fmtDate(`${date.year}-${String(date.month + 1).padStart(2, "0")}-${String(date.day).padStart(2, "0")}`);
}

/** Short weekday for a FullCalendar date (a bare weekday is not a date, so it keeps its own form). */
function weekdayOf(date: FcDateParts): string {
  return new Intl.DateTimeFormat("en", { weekday: "short", timeZone: "UTC" }).format(Date.UTC(date.year, date.month, date.day));
}

function toIsoDate(date: Date): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}
