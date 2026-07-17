import Link from "@/components/no-prefetch-link";
import { CalendarDays, ChevronLeft, ChevronRight, Clock, Info } from "lucide-react";
import { actionFeedbackCopy, copy, optionGroup, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, hrefWithoutAction, hrefWithPagedCursor, hrefPreviousPagedCursor, boundedInt, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { todayIso } from "@/lib/format";
import { Tag } from "@/components/ui-primitives";
import {
  eventTypeMeta,
  fallbackCalendarPresentation,
  ownerColor,
  ownerLabel,
  ownerMetaFromPresentation,
  ownerScopeLabel,
  presentationQueryToSearch,
  type CalendarDateMarker,
  type CalendarEvent,
  type CalendarOwnerFilter,
  type CalendarOwnerPresentationTab,
  type CalendarPresentation,
  type CalendarPresentationTab,
  type CalendarRhythmDay,
  type OwnerPresentationMap,
} from "./calendar-contract";
import { getCalendarVaccinationEvents, getCalendarVaccinationEventDetail, getCalendarDriveTargets } from "./calendar-server";
import { CalendarEventDrawer } from "./calendar-event-drawer";
import { CalendarMonthPicker } from "./calendar-month-picker";
import { markerTonesForDate } from "./calendar-marker-tones";
import { calendarMonthAnchor, enumerateWeekDays, historyWindow, monthWindow, shiftedMonthStartKey, weekWindow } from "./calendar-window";
import {
  calendarEventDateKey,
  calendarEventWeekday,
  filterEventsForSelectedWeek,
  normalizeCalendarDayFilter,
} from "./calendar-window-events";
import { DriveProgressCard } from "./calendar-drive-card";
import { OwnerLegend, RhythmCard } from "./calendar-week-panels";

const PATH = "/calendar";

function weekdayOf(iso: string): string {
  return calendarEventWeekday(iso);
}

function istToday(): string {
  return todayIso();
}

function timeOf(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso.slice(11, 16) || "—";
  return new Intl.DateTimeFormat("en-GB", { timeZone: "Asia/Kolkata", hour: "2-digit", minute: "2-digit", hour12: false }).format(d);
}

function dateKey(iso: string): string {
  return calendarEventDateKey(iso);
}

function dateHeading(key: string, today: string, pageContract: AdminUiPageContract): string {
  const d = new Date(`${key}T00:00:00+05:30`);
  if (Number.isNaN(d.getTime())) return key;
  const wd = optionLabel(pageContract, "calendar_weekdays", String(d.getDay())).toUpperCase();
  if (key === today) return `${wd} · ${copy(pageContract, "label.today")}`;
  return `${wd} · ${optionLabel(pageContract, "calendar_months", String(d.getMonth())).slice(0, 3).toUpperCase()} ${d.getDate()}`;
}

function compactDateLabel(key: string, pageContract: AdminUiPageContract): string {
  const d = new Date(`${key}T00:00:00+05:30`);
  if (Number.isNaN(d.getTime())) return key;
  return `${optionLabel(pageContract, "calendar_months", String(d.getMonth())).slice(0, 3)} ${d.getDate()}`;
}

function addDaysKey(key: string, days: number): string {
  const d = new Date(`${key}T00:00:00+05:30`);
  if (Number.isNaN(d.getTime())) return key;
  d.setDate(d.getDate() + days);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function EventRow({
  event,
  href,
  driveHref,
  ownerMeta,
  pageContract,
}: {
  event: CalendarEvent;
  href: string;
  driveHref?: (id: string) => string;
  ownerMeta: OwnerPresentationMap;
  pageContract: AdminUiPageContract;
}) {
  const meta = [
    optionLabel(pageContract, "calendar_status", event.status),
    event.subtitle,
    event.park_code,
    event.shed_name,
    ownerLabel(event.owner_key, ownerMeta),
  ]
    .filter(Boolean)
    .join(" · ");
  // summary_tertiary often repeats subtitle/shed_name verbatim (e.g. a single-shed drive's tertiary
  // line is just that shed's name again) — only render it as a second meta line when it adds
  // information the first `.em` line doesn't already carry.
  const showTertiary = event.summary_tertiary && event.summary_tertiary !== event.subtitle && event.summary_tertiary !== event.shed_name;

  if (event.aggregated) {
    // Drive rows: render the card as a link (NO wrapping in .ev/.et/.eb).
    // Navigate to the drive detail page with scope (park/shed/as_of) preserved.
    return (
      <Link href={driveHref ? driveHref(event.event_id) : ""} replace scroll={false} className="drivelink">
        <DriveProgressCard event={event} pageContract={pageContract} />
      </Link>
    );
  }

  return (
    <Link href={href} replace scroll={false} className="ev celllink" style={{ borderLeftColor: ownerColor(event.owner_key, ownerMeta) }}>
      <div className="et">{event.all_day ? copy(pageContract, "calendar.drive.all_day") : timeOf(event.due_at)}</div>
      <div className="eb">
        <b>{event.title}</b>
        <div className="em">{meta}</div>
        {showTertiary ? <div className="em" style={{ marginTop: 7 }}>{event.summary_tertiary}</div> : null}
      </div>
    </Link>
  );
}

export async function VaccinationCalendarPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const requestedStatus = one(sp, "status") === "completed" ? "completed" : undefined;
  const historyMode = requestedStatus === "completed";
  const datePickerOpen = one(sp, "view") === "month";
  const currentViewKey = historyMode ? "history" : datePickerOpen ? "month" : "week";
  const requestedOwnerKey = (one(sp, "owner_key") || "all") as CalendarOwnerFilter;
  const selectedEventId = one(sp, "event");
  const listCursor = one(sp, "cursor");
  const listPage = boundedInt(one(sp, "page"), 1, 1, 1000);
  const targetsCursor = one(sp, "targets_cursor");
  const targetsPage = boundedInt(one(sp, "targets_page"), 1, 1, 1000);
  const today = istToday();
  const dayFilter = normalizeCalendarDayFilter(one(sp, "day") || undefined);
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");

  const anchorKey = asOf ? asOf.slice(0, 10) : today;
  // Day-wise week view: the rhythm strip is the day selector. A specific day shows ONLY that
  // day's drives (no redundant day heading — the strip already names the day). "All week"
  // (day=week) opts into the 7-day, day-separated vertical list. Default = single day, defaulted
  // to the anchor day's weekday (the as-of / picked date). History never defaults a day.
  // NB: the sentinel is "week", NOT "all" — scopeHref() drops any query value equal to "all",
  // so an "all" sentinel would be silently stripped and the toggle would never engage.
  const anchorWeekday = weekdayOf(`${anchorKey}T00:00:00+05:30`);
  const allWeek = !historyMode && dayFilter === "week";
  const effectiveDayFilter = historyMode || allWeek ? undefined : dayFilter ?? anchorWeekday;
  const dayForHref = allWeek ? "week" : effectiveDayFilter;
  const pickerWindow = monthWindow(anchorKey);
  const agendaWindow = weekWindow(anchorKey);
  const completedWindow = historyWindow(anchorKey);
  const listWindow = historyMode ? completedWindow : agendaWindow;

  // The month-marker request is useful only while the picker is open. Keep it
  // concurrent with the agenda request when needed, but do not double-read the
  // calendar API on every normal week/history navigation.
  const markerRequest = datePickerOpen
    ? getCalendarVaccinationEvents({
        parkId,
        ownerKey: requestedOwnerKey,
        status: historyMode ? requestedStatus : undefined,
        dateFrom: pickerWindow.dateFrom,
        dateTo: pickerWindow.dateTo,
        includeDateMarkers: true,
        markersOnly: true,
        limit: 1,
      })
    : Promise.resolve(null);
  const [list, markers, detail, targets] = await Promise.all([
    getCalendarVaccinationEvents({
      parkId,
      ownerKey: requestedOwnerKey,
      status: requestedStatus,
      dateFrom: listWindow.dateFrom,
      dateTo: listWindow.dateTo,
      cursor: listCursor,
    }),
    markerRequest,
    selectedEventId ? getCalendarVaccinationEventDetail(selectedEventId) : Promise.resolve(null),
    selectedEventId && !historyMode ? getCalendarDriveTargets(selectedEventId, { cursor: targetsCursor, limit: 10 }) : Promise.resolve(null),
  ]);

  const events = list.ok ? list.data.items : [];
  const pickerEvents = markers?.ok ? markers.data.items : events;
  if (list.ok && !list.data.presentation) {
    throw new Error(copy(pageContract, "error.presentation_missing"));
  }
  // Presentation comes from the main list request; markers request is presentation-independent
  const presentation = list.ok && list.data.presentation ? list.data.presentation : fallbackCalendarPresentation(pageContract, requestedOwnerKey);
  const ownerMeta = ownerMetaFromPresentation(presentation);
  const activeOwnerKey = presentation.active_owner_key as CalendarOwnerFilter;
  const historyTabLabel = presentation.view_tabs.find((tab) => tab.key === "history")?.label || optionLabel(pageContract, "calendar_view_tabs", "history");
  const weekTabLabel = presentation.view_tabs.find((tab) => tab.key === "week")?.label || optionLabel(pageContract, "calendar_view_tabs", "week");
  const monthTabLabel = presentation.view_tabs.find((tab) => tab.key === "month")?.label || optionLabel(pageContract, "calendar_view_tabs", "month");

  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref(PATH, scope, {}, {
      view: datePickerOpen ? "month" : undefined,
      owner_key: activeOwnerKey === "all" ? undefined : activeOwnerKey,
      status: requestedStatus,
      day: dayForHref,
      cursor: listCursor,
      page: listPage > 1 ? String(listPage) : undefined,
      ...overrides,
    });
  }

  const hrefForOwnerTab = (tab: CalendarOwnerPresentationTab) =>
    hrefWith({
      ...presentationQueryToSearch(tab.query),
      event: undefined,
      cursor: undefined,
      page: undefined,
      cursor_stack: undefined,
      targets_cursor: undefined,
      targets_page: undefined,
      targets_cursor_stack: undefined,
    });
  const hrefForWorkstreamTab = (tab: CalendarPresentationTab) =>
    hrefWith({
      ...presentationQueryToSearch(tab.query),
      event: undefined,
      cursor: undefined,
      page: undefined,
      cursor_stack: undefined,
      targets_cursor: undefined,
      targets_page: undefined,
      targets_cursor_stack: undefined,
    });
  const eventHref = (id: string) => hrefWith({ event: id, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const driveHref = (id: string) => scopeHref(`/calendar/drive/${encodeURIComponent(id)}`, scope);
  const closeHref = hrefWith({ event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const weekHref = hrefWith({
    status: undefined,
    view: undefined,
    event: undefined,
    cursor: undefined,
    page: undefined,
    cursor_stack: undefined,
    targets_cursor: undefined,
    targets_page: undefined,
    targets_cursor_stack: undefined,
  });
  const previousWeekHref = hrefWith({
    as_of: addDaysKey(anchorKey, -7),
    status: undefined,
    view: undefined,
    event: undefined,
    cursor: undefined,
    page: undefined,
    cursor_stack: undefined,
    targets_cursor: undefined,
    targets_page: undefined,
    targets_cursor_stack: undefined,
  });
  const currentWeekHref = hrefWith({
    as_of: today,
    status: undefined,
    view: undefined,
    event: undefined,
    cursor: undefined,
    page: undefined,
    cursor_stack: undefined,
    targets_cursor: undefined,
    targets_page: undefined,
    targets_cursor_stack: undefined,
  });
  const nextWeekHref = hrefWith({
    as_of: addDaysKey(anchorKey, 7),
    status: undefined,
    view: undefined,
    event: undefined,
    cursor: undefined,
    page: undefined,
    cursor_stack: undefined,
    targets_cursor: undefined,
    targets_page: undefined,
    targets_cursor_stack: undefined,
  });
  const historyHref = hrefWith({
    status: "completed",
    view: undefined,
    day: undefined,
    event: undefined,
    cursor: undefined,
    page: undefined,
    cursor_stack: undefined,
    targets_cursor: undefined,
    targets_page: undefined,
    targets_cursor_stack: undefined,
  });
  const pickerMonthHref = (date: string) =>
    hrefWith({
      as_of: date,
      view: "month",
      day: undefined,
      event: undefined,
      cursor: undefined,
      page: undefined,
      cursor_stack: undefined,
      targets_cursor: undefined,
      targets_page: undefined,
      targets_cursor_stack: undefined,
    });
  const pickerDateHref = (date: string, historyOnly = false) =>
    hrefWith({
      as_of: date,
      status: historyOnly ? "completed" : undefined,
      view: undefined,
      day: undefined,
      event: undefined,
      cursor: undefined,
      page: undefined,
      cursor_stack: undefined,
      targets_cursor: undefined,
      targets_page: undefined,
      targets_cursor_stack: undefined,
    });

  const sel = detail && detail.ok ? detail.data : null;
  const spForTargets: RouteSearchParams = { ...sp, event: selectedEventId };
  const spForList: RouteSearchParams = {
    ...sp,
    event: undefined,
    targets_cursor: undefined,
    targets_page: undefined,
    targets_cursor_stack: undefined,
  };

  function scopedHref(href: string | null): string | null {
    if (!href) return null;
    const [path, query = ""] = href.split("?");
    const extra = Object.fromEntries(new URLSearchParams(query));
    return scopeHref(path, scope, {}, extra);
  }

  const listNextHref =
    list.ok && list.data.next_cursor
      ? scopedHref(hrefWithPagedCursor(PATH, spForList, "cursor", list.data.next_cursor, "page", "cursor_stack"))
      : null;
  const listPrevHref = scopedHref(hrefPreviousPagedCursor(PATH, spForList, "cursor", "page", "cursor_stack"));
  const listOnPage = list.ok ? list.data.items.length : 0;
  const targetsNextHref =
    targets && targets.ok && targets.data.next_cursor
      ? scopedHref(hrefWithPagedCursor(PATH, spForTargets, "targets_cursor", targets.data.next_cursor, "targets_page", "targets_cursor_stack"))
      : null;
  const targetsPrevHref = scopedHref(hrefPreviousPagedCursor(PATH, spForTargets, "targets_cursor", "targets_page", "targets_cursor_stack"));
  const targetsOnPage = targets && targets.ok ? targets.data.items.length : 0;

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <h1>{presentation.page_title || pageContract.title}</h1>
          <div className="sub">{presentation.page_subtitle || pageContract.subtitle}</div>
        </div>
        <div className="sp" />
        <div className="subtabs calview-tabs" style={{ margin: 0 }}>
          <Link href={weekHref} replace scroll={false} className={currentViewKey === "week" ? "on" : ""}>
            {weekTabLabel}
          </Link>
          <CalendarMonthPicker label={monthTabLabel} open={currentViewKey === "month"} closeHref={historyMode ? historyHref : weekHref}>
            <CalendarDatePicker
              events={pickerEvents}
              dateMarkers={markers?.ok ? markers.data.date_markers : []}
              anchorKey={anchorKey}
              today={today}
              presentation={presentation}
              pageContract={pageContract}
              monthHref={pickerMonthHref}
              dateHref={pickerDateHref}
            />
          </CalendarMonthPicker>
          <Link href={historyHref} replace scroll={false} className={currentViewKey === "history" ? "on" : ""}>
            {historyTabLabel}
          </Link>
        </div>
      </div>

      <div className="subtabs" style={{ margin: "0 0 10px" }} aria-label={copy(pageContract, "filter.owner.aria")}>
        {presentation.owner_tabs.map((tab) => (
          <Link
            key={tab.key}
            href={hrefForOwnerTab(tab)}
            replace
            scroll={false}
            className={activeOwnerKey === tab.key || tab.active ? "on" : tab.enabled ? "" : "disabled"}
            aria-disabled={tab.enabled ? undefined : "true"}
            title={tab.enabled ? undefined : tab.disabled_reason}
          >
            {tab.label}
          </Link>
        ))}
      </div>

      {presentation.workstream_tabs.length ? (
        <div className="subtabs" style={{ margin: "0 0 14px" }} aria-label={copy(pageContract, "filter.workstream.aria")}>
          {presentation.workstream_tabs.map((tab) =>
            tab.enabled && !tab.active ? (
              <Link key={tab.key} href={hrefForWorkstreamTab(tab)} replace scroll={false} title={tab.disabled_reason || undefined}>
                {tab.label}
              </Link>
            ) : (
              <button
                key={tab.key}
                type="button"
                className={tab.active ? "on" : "disabled"}
                aria-current={tab.active ? "true" : undefined}
                aria-disabled={tab.enabled ? undefined : "true"}
                title={tab.disabled_reason || undefined}
              >
                {tab.label}
              </button>
            ),
          )}
        </div>
      ) : null}

      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            <Tag tone="ok">{copy(pageContract, "action.success_tag")}</Tag> {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
            <b>{copy(pageContract, "action.failed_title")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        )
      ) : null}

      {!list.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{list.error.code ?? list.error.kind}</b>&nbsp;{list.error.message}
        </div>
      ) : null}
      {selectedEventId && detail && !detail.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{detail.error.code ?? detail.error.kind}</b>&nbsp;{detail.error.message}
        </div>
      ) : null}

      {historyMode ? (
        <HistoryView
          events={events}
          today={today}
          eventHref={eventHref}
          driveHref={driveHref}
          ownerKey={activeOwnerKey}
          ownerMeta={ownerMeta}
          pageContract={pageContract}
          title={historyTabLabel}
        />
      ) : (
        <WeekView
          events={events}
          today={today}
          anchorDay={anchorKey}
          allWeek={allWeek}
          eventHref={eventHref}
          driveHref={driveHref}
          ownerKey={activeOwnerKey}
          presentation={presentation}
          ownerMeta={ownerMeta}
          clearOwnerHref={hrefWith({ owner_key: undefined, event: undefined, cursor: undefined, page: undefined, cursor_stack: undefined })}
          dayFilter={effectiveDayFilter}
          allWeekHref={hrefWith({ day: "week", event: undefined, cursor: undefined, page: undefined, cursor_stack: undefined })}
          previousWeekHref={previousWeekHref}
          currentWeekHref={currentWeekHref}
          nextWeekHref={nextWeekHref}
          rhythmDayHref={(day) =>
            hrefWith({ ...presentationQueryToSearch(day.query), event: undefined, cursor: undefined, page: undefined, cursor_stack: undefined })
          }
          pageContract={pageContract}
        />
      )}

      {historyMode && events.length === 0 ? <EmptyState ok={list.ok} presentation={presentation} mode="history" pageContract={pageContract} /> : null}
      {list.ok ? <EventPager nextHref={listNextHref} prevHref={listPrevHref} page={listPage} rowsOnPage={listOnPage} pageContract={pageContract} /> : null}

      {sel ? (
        <CalendarEventDrawer
          detail={sel}
          targets={targets && targets.ok ? targets.data.items : null}
          targetsError={targets && !targets.ok ? targets.error.message : null}
          targetsNextHref={targetsNextHref}
          targetsPrevHref={targetsPrevHref}
          targetsPage={targetsPage}
          targetsOnPage={targetsOnPage}
          closeHref={closeHref}
          returnTo={hrefWithoutAction(PATH, sp)}
          scope={scope}
          presentation={presentation}
          ownerMeta={ownerMeta}
          pageContract={pageContract}
        />
      ) : null}
    </div>
  );
}

function WeekView({
  events,
  today,
  anchorDay,
  allWeek,
  eventHref,
  driveHref,
  ownerKey,
  presentation,
  ownerMeta,
  clearOwnerHref,
  dayFilter,
  allWeekHref,
  previousWeekHref,
  currentWeekHref,
  nextWeekHref,
  rhythmDayHref,
  pageContract,
}: {
  events: CalendarEvent[];
  today: string;
  anchorDay: string;
  allWeek: boolean;
  eventHref: (id: string) => string;
  driveHref: (id: string) => string;
  ownerKey: CalendarOwnerFilter;
  presentation: CalendarPresentation;
  ownerMeta: OwnerPresentationMap;
  clearOwnerHref: string;
  dayFilter?: string;
  allWeekHref: string;
  previousWeekHref: string;
  currentWeekHref: string;
  nextWeekHref: string;
  rhythmDayHref: (day: CalendarRhythmDay) => string;
  pageContract: AdminUiPageContract;
}) {
  // Rhythm strip is the day selector. A specific weekday shows ONLY that day's drives, with NO
  // day heading in the body (the strip already names the day). "All week" (allWeek) opts into the
  // 7-day, day-separated vertical list with per-day headings.
  const scoped = filterEventsForSelectedWeek(events, anchorDay, dayFilter);
  const sorted = [...scoped].sort((a, b) => a.due_at.localeCompare(b.due_at));
  const selectedOwnerLabel = ownerScopeLabel(ownerKey, ownerMeta);
  const todayWeekday = weekdayOf(`${today}T00:00:00+05:30`);
  const weekDays = enumerateWeekDays(weekWindow(anchorDay).dateFrom);
  const dateLabelByDay = Object.fromEntries(
    weekDays.map((dayDate) => [weekdayOf(`${dayDate}T00:00:00+05:30`), compactDateLabel(dayDate, pageContract)]),
  );
  // Selected-day Sunday check drives the rest-day empty copy — derived from the actual date,
  // not a weekday string literal (which the admin-ui contract-literal guard forbids).
  const selectedDate = dayFilter ? weekDays.find((d) => weekdayOf(`${d}T00:00:00+05:30`) === dayFilter) : undefined;
  const selectedIsSunday = selectedDate ? new Date(`${selectedDate}T00:00:00+05:30`).getDay() === 0 : false;

  // Per-day buckets — only consumed by the all-week vertical list.
  const byDate = new Map<string, CalendarEvent[]>();
  for (const event of sorted) {
    const key = dateKey(event.due_at);
    const bucket = byDate.get(key) ?? [];
    bucket.push(event);
    byDate.set(key, bucket);
  }

  return (
    <>
      <RhythmCard
        rhythmDayHref={rhythmDayHref}
        todayWeekday={todayWeekday}
        dayFilter={dayFilter}
        allWeek={allWeek}
        allWeekHref={allWeekHref}
        previousWeekHref={previousWeekHref}
        currentWeekHref={currentWeekHref}
        nextWeekHref={nextWeekHref}
        previousWeekLabel={copy(pageContract, "action.previous")}
        currentWeekLabel={presentation.week.title}
        nextWeekLabel={copy(pageContract, "action.next")}
        dateLabelByDay={dateLabelByDay}
        presentation={presentation}
      />
      <div className="card">
        <div className="hd">
          <CalendarDays className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{presentation.week.title}</h3>
          <span className="sp" />
          <OwnerLegend ownerMeta={ownerMeta} />
        </div>
        <div className="bd">
          {ownerKey !== "all" ? (
            <div className="fchipsbar calband">
              {presentation.week.scope_only_message || (
                <>
                  {copy(pageContract, "week.showing_prefix")} <b>{selectedOwnerLabel}</b> {copy(pageContract, "week.work_only_suffix")}
                </>
              )}
              <Link href={clearOwnerHref} replace scroll={false} className="lenslink">
                ↺ {presentation.week.clear_scope_label}
              </Link>
            </div>
          ) : null}
          <div className="agenda">
            {allWeek ? (
              weekDays.map((dayDate) => {
                const rows = byDate.get(dateKey(dayDate)) ?? [];
                const isSunday = new Date(`${dayDate}T00:00:00+05:30`).getDay() === 0;
                return (
                  <div key={dayDate}>
                    <div className={dayDate === today ? "dh today" : "dh"}>{dateHeading(dateKey(dayDate), today, pageContract)}</div>
                    {rows.length === 0 ? (
                      <div className="cal-empty">
                        {copy(pageContract, isSunday ? "calendar.week.rest_day" : "calendar.week.day_empty")}
                      </div>
                    ) : (
                      rows.map((event) => (
                        <EventRow key={event.event_id} event={event} href={eventHref(event.event_id)} driveHref={driveHref} ownerMeta={ownerMeta} pageContract={pageContract} />
                      ))
                    )}
                  </div>
                );
              })
            ) : sorted.length === 0 ? (
              <div>
                {selectedDate ? <div className={selectedDate === today ? "dh today" : "dh"}>{dateHeading(selectedDate, today, pageContract)}</div> : null}
                <div className="cal-empty">
                  {copy(pageContract, selectedIsSunday ? "calendar.week.rest_day" : "calendar.week.day_empty")}
                </div>
              </div>
            ) : (
              <div>
                {selectedDate ? <div className={selectedDate === today ? "dh today" : "dh"}>{dateHeading(selectedDate, today, pageContract)}</div> : null}
                {sorted.map((event) => (
                  <EventRow key={event.event_id} event={event} href={eventHref(event.event_id)} driveHref={driveHref} ownerMeta={ownerMeta} pageContract={pageContract} />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </>
  );
}

function HistoryView({
  events,
  today,
  eventHref,
  driveHref,
  ownerKey,
  ownerMeta,
  pageContract,
  title,
}: {
  events: CalendarEvent[];
  today: string;
  eventHref: (id: string) => string;
  driveHref: (id: string) => string;
  ownerKey: CalendarOwnerFilter;
  ownerMeta: OwnerPresentationMap;
  pageContract: AdminUiPageContract;
  title: string;
}) {
  const sorted = [...events].sort((a, b) => b.due_at.localeCompare(a.due_at));
  const byDate = new Map<string, CalendarEvent[]>();
  for (const event of sorted) {
    const key = dateKey(event.due_at);
    const bucket = byDate.get(key) ?? [];
    bucket.push(event);
    byDate.set(key, bucket);
  }
  return (
    <div className="card">
      <div className="hd">
        <Clock className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{title}</h3>
        <Tag tone={ownerKey === "all" ? "mut" : "info"}>{ownerScopeLabel(ownerKey, ownerMeta)}</Tag>
      </div>
      <div className="bd">
        <div className="agenda">
          {Array.from(byDate.entries()).map(([key, rows]) => (
            <div key={key}>
              <div className="dh">{dateHeading(key, today, pageContract)}</div>
              {rows.map((event) => (
                <EventRow key={event.event_id} event={event} href={eventHref(event.event_id)} driveHref={driveHref} ownerMeta={ownerMeta} pageContract={pageContract} />
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function EventPager({
  nextHref,
  prevHref,
  page,
  rowsOnPage,
  pageContract,
}: {
  nextHref: string | null;
  prevHref: string | null;
  page: number;
  rowsOnPage: number;
  pageContract: AdminUiPageContract;
}) {
  if (!nextHref && !prevHref) return null;
  return (
    <div className="subtabs" style={{ marginTop: 14, justifyContent: "space-between" }}>
      {prevHref ? (
        <Link href={prevHref} replace scroll={false}>
          {copy(pageContract, "action.previous")}
        </Link>
      ) : (
        <span className="disabled">{copy(pageContract, "action.previous")}</span>
      )}
      <span className="muted small">
        {copy(pageContract, "pager.page")} {page} · {rowsOnPage} {copy(pageContract, "pager.rows").toLowerCase()}
      </span>
      {nextHref ? (
        <Link href={nextHref} replace scroll={false}>
          {copy(pageContract, "action.next")}
        </Link>
      ) : (
        <span className="disabled">{copy(pageContract, "action.next")}</span>
      )}
    </div>
  );
}

function CalendarDatePicker({
  events,
  dateMarkers,
  anchorKey,
  today,
  presentation,
  pageContract,
  monthHref,
  dateHref,
}: {
  events: CalendarEvent[];
  dateMarkers: CalendarDateMarker[];
  anchorKey: string;
  today: string;
  presentation: CalendarPresentation;
  pageContract: AdminUiPageContract;
  monthHref: (date: string) => string;
  dateHref: (date: string, historyOnly?: boolean) => string;
}) {
  const { year, month } = calendarMonthAnchor(anchorKey, today);
  const weekdays = optionGroup(pageContract, "calendar_weekdays");

  const byDate = new Map<string, CalendarEvent[]>();
  for (const event of events) {
    const key = dateKey(event.due_at);
    const bucket = byDate.get(key) ?? [];
    bucket.push(event);
    byDate.set(key, bucket);
  }
  const markerByDate = new Map<string, CalendarDateMarker>();
  for (const marker of dateMarkers) {
    markerByDate.set(marker.date, marker);
  }
  const firstDow = new Date(year, month, 1).getDay();
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const cells: ({ day: number; key: string } | null)[] = [];
  for (let i = 0; i < firstDow; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) {
    cells.push({ day: d, key: `${year}-${String(month + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}` });
  }
  const previousMonth = shiftedMonthStartKey(year, month, -1);
  const nextMonth = shiftedMonthStartKey(year, month, 1);
  const previousYearMonth = shiftedMonthStartKey(year, month, -12);
  const nextYearMonth = shiftedMonthStartKey(year, month, 12);
  const previousLabel = copy(pageContract, "calendar.picker.previous_month");
  const nextLabel = copy(pageContract, "calendar.picker.next_month");
  const previousYearLabel = copy(pageContract, "calendar.picker.previous_year");
  const nextYearLabel = copy(pageContract, "calendar.picker.next_year");
  const monthLinks = Array.from({ length: 12 }, (_, i) => `${year}-${String(i + 1).padStart(2, "0")}-01`);

  return (
    <div className="calpicker-popover">
      <div className="calpicker-head">
        <Link href={monthHref(previousMonth)} replace scroll={false} className="calpicker-nav" aria-label={previousLabel}>
          <ChevronLeft className="ic" aria-hidden="true" />
        </Link>
        <div className="calpicker-title">
          <b>{optionLabel(pageContract, "calendar_months", String(month))}</b>
          <span>{year}</span>
        </div>
        <Link href={monthHref(nextMonth)} replace scroll={false} className="calpicker-nav" aria-label={nextLabel}>
          <ChevronRight className="ic" aria-hidden="true" />
        </Link>
      </div>
      <div className="calpicker-yearnav">
        <Link href={monthHref(previousYearMonth)} replace scroll={false} aria-label={previousYearLabel}>
          ‹ {year - 1}
        </Link>
        <Link href={monthHref(nextYearMonth)} replace scroll={false} aria-label={nextYearLabel}>
          {year + 1} ›
        </Link>
      </div>
      <div className="calpicker-months">
        {monthLinks.map((monthKey, index) => (
          <Link
            key={monthKey}
            href={monthHref(monthKey)}
            replace
            scroll={false}
            className={index === month ? "selected" : ""}
          >
            {optionLabel(pageContract, "calendar_months", String(index)).slice(0, 3)}
          </Link>
        ))}
      </div>
      <div className="calpicker-grid">
        {weekdays.map((weekday) => (
          <span key={weekday.key} className="calpicker-dow">
            {weekday.label.slice(0, 3)}
          </span>
        ))}
        {cells.map((cell, index) => {
          if (cell === null) return <span key={`out-${index}`} className="calpicker-day out" />;
          const dayEvents = byDate.get(cell.key) ?? [];
          const marker = markerByDate.get(cell.key);
          const hasDrive = dayEvents.length > 0;
          const hasHistory = (marker?.completed_count ?? 0) > 0;
          const hasOpenWork = (marker?.open_count ?? 0) > 0;
          const tones = markerTonesForDate(marker, dayEvents).slice(0, 1);
          const eventCount = marker?.event_count ?? dayEvents.length;
          return (
            <Link
              key={cell.key}
              href={dateHref(cell.key, hasHistory && !hasOpenWork)}
              replace
              scroll={false}
              className={`calpicker-day${cell.key === today ? " today" : ""}${cell.key === anchorKey ? " selected" : ""}${eventCount ? " has-events" : ""}${hasDrive ? " has-drive" : ""}${hasHistory ? " has-history" : ""}${hasOpenWork ? " has-open-work" : ""}`}
              title={
                dayEvents.length
                  ? dayEvents
                      .slice(0, 4)
                      .map((event) => `${event.all_day ? copy(pageContract, "calendar.drive.all_day") : timeOf(event.due_at)} ${eventTypeMeta(event.event_type, presentation).label} - ${event.title}`)
                      .join("\n")
                  : undefined
              }
            >
              <span>{cell.day}</span>
              {tones.length ? (
                <i className="calmarker-dots" aria-hidden="true">
                  {tones.map((tone) => (
                    <em key={tone} className={`tone-${tone}`} />
                  ))}
                </i>
              ) : null}
            </Link>
          );
        })}
      </div>
      <div className="calpicker-note">
        <span className="calpicker-dot drive" /> {copy(pageContract, "calendar.picker.drive_hint")}
        <span className="calpicker-dot due" /> {copy(pageContract, "calendar.picker.due_hint")}
        <span className="calpicker-dot deferred" /> {copy(pageContract, "calendar.picker.deferred_hint")}
        <span className="calpicker-dot other" /> {copy(pageContract, "calendar.picker.other_hint")}
        <span className="calpicker-dot history" /> {copy(pageContract, "calendar.picker.history_hint")}
        <span className="sp" />
        <span>{presentation.month.as_of_hint}</span>
      </div>
    </div>
  );
}

function EmptyState({
  ok,
  presentation,
  mode,
  pageContract,
}: {
  ok: boolean;
  presentation: CalendarPresentation;
  mode: "week" | "history";
  pageContract: AdminUiPageContract;
}) {
  const okTitle = mode === "history" ? copy(pageContract, "calendar.history.empty") : presentation.week.empty_message;
  const okBody = mode === "history" ? copy(pageContract, "calendar.history.empty_note") : presentation.empty_state.ok_message;
  return (
    <div className="calempty" role="status">
      <span className="calempty-ic">
        {ok ? <CalendarDays className="ic" aria-hidden="true" /> : <Info className="ic" aria-hidden="true" />}
      </span>
      <div className="calempty-copy">
        <b>{ok ? okTitle : presentation.empty_state.error_message}</b>
        <span>{ok ? okBody : presentation.empty_state.error_message}</span>
      </div>
      {ok ? <Tag tone="mut">{presentation.active_owner_scope_label}</Tag> : null}
    </div>
  );
}
