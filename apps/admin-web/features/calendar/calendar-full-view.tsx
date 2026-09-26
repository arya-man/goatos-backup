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

import { useRouter } from "next/navigation";

import { CalendarRoot, CalendarToolbar, type CalendarView } from "@/components/minimal/calendar";
import { pushLocalOverlayUrl } from "@/components/local-overlay-link";
import { scopeHref, type Scope } from "@/lib/scope";

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
  const calendarRef = useRef<FullCalendar | null>(null);
  const smUp = useMediaQuery<Theme>((theme) => theme.breakpoints.up("sm"));

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
    setTitle(api.view.title);
  }, [smUp]);

  const onChangeView = useCallback((next: CalendarView) => {
    const api = calendarRef.current?.getApi();
    if (!api) return;
    api.changeView(next);
    setView(next);
    setTitle(api.view.title);
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
      setTitle(api.view.title);
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
        // Explicit tall min-height because the page tree above uses CSS grid, not a flex column,
        // so `flex: 1 1 auto` alone cannot stretch the Card to fill the viewport (template pattern
        // relies on DashboardContent -> flex column chain). Six-week month grid + toolbar needs
        // ~640px at 1440-wide desktop; 100dvh-220px keeps the whole month visible on laptop and
        // most webviews without clipping.
        minHeight: { xs: "70vh", md: "calc(100dvh - 220px)" },
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

        <Box sx={{ flex: "1 1 auto", display: "flex", flexDirection: "column", "& .fc": { flex: "1 1 auto" } }}>
          <FullCalendar
            ref={calendarRef}
            firstDay={1}
            weekends
            aspectRatio={3}
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
            datesSet={(arg) => setTitle(arg.view.title)}
            plugins={[dayGridPlugin, timeGridPlugin, listPlugin]}
            businessHours={{ daysOfWeek: [1, 2, 3, 4, 5] }}
            noEventsText={noEventsText}
          />
        </Box>
      </CalendarRoot>
    </Card>
  );
}

function toIsoDate(date: Date): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}
