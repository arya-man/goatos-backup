import Link from "next/link";
import { CalendarDays, ChevronLeft, ChevronRight, Info } from "lucide-react";
import { actionFeedbackCopy, copy, optionGroup, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, hrefWithoutAction, hrefWithPagedCursor, hrefPreviousPagedCursor, boundedInt, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { fmtDate as fmtIstDate, todayIso } from "@/lib/format";
import { Tag } from "@/components/ui-primitives";
import {
  eventTypeMeta,
  fallbackCalendarPresentation,
  ownerColor,
  ownerLabel,
  ownerMetaFromPresentation,
  ownerScopeLabel,
  presentationQueryToSearch,
  type CalendarEvent,
  type CalendarDateMarker,
  type CalendarOwnerFilter,
  type CalendarOwnerPresentationTab,
  type CalendarPresentation,
  type CalendarPresentationTab,
  type OwnerPresentationMap,
} from "./calendar-contract";
import { getCalendarVaccinationEvents, getCalendarVaccinationEventDetail, getCalendarDriveTargets } from "./calendar-server";
import { CalendarEventDrawer } from "./calendar-event-drawer";
import { CalendarDriveSummaryDrawer, type CalendarDriveSummaryEvent } from "./calendar-drive-summary-drawer";
import { CalendarMonthPicker } from "./calendar-month-picker";
import { monthWindow, weekWindow } from "./calendar-window";

const PATH = "/calendar";

// IST weekday short name for an arbitrary event instant (for the day filter).
function weekdayOf(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const wd = new Intl.DateTimeFormat("en-US", { timeZone: "Asia/Kolkata", weekday: "short" }).format(d);
  return wd; // "Mon".."Sun"
}

// Business "today" in the operating-tenant timezone (IST). Used for the month highlight + agenda "TODAY".
function istToday(): string {
  return todayIso();
}

function timeOf(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso.slice(11, 16) || "—";
  return new Intl.DateTimeFormat("en-GB", { timeZone: "Asia/Kolkata", hour: "2-digit", minute: "2-digit", hour12: false }).format(d);
}

function dateKey(iso: string): string {
  // Bucket by IST calendar day (events carry an absolute instant; group by the day they fall on in IST).
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso.slice(0, 10);
  return fmtIstDate(iso);
}

function dateHeading(key: string, today: string, pageContract: AdminUiPageContract): string {
  const d = new Date(`${key}T00:00:00+05:30`);
  if (Number.isNaN(d.getTime())) return key;
  const wd = optionLabel(pageContract, "calendar_weekdays", String(d.getDay())).toUpperCase();
  if (key === today) return `${wd} · ${copy(pageContract, "label.today")}`;
  return `${wd} · ${optionLabel(pageContract, "calendar_months", String(d.getMonth())).slice(0, 3).toUpperCase()} ${d.getDate()}`;
}

function asDriveSummaryEvent(event: CalendarEvent): CalendarDriveSummaryEvent {
  return event as CalendarDriveSummaryEvent;
}

function uniqueStrings(values: Array<string | null | undefined>): string[] {
  return Array.from(
    new Set(
      values
        .map((value) => value?.trim())
        .filter((value): value is string => Boolean(value)),
    ),
  );
}

function genericScopeLabel(value: string | null | undefined): boolean {
  const normalized = (value || "").trim().toLowerCase();
  return normalized === "" || normalized === "catch-up drive" || normalized === "vaccination drive" || normalized === "review queue";
}

function driveStage(event: CalendarEvent): "drive" | "review" | "skip" {
  switch (event.event_type) {
    case "vaccination_defer_review":
    case "vaccination_evidence_review":
    case "vaccination_proof_verification":
    case "vaccination_rework_due":
    case "vaccination_config_activation_review":
      return "review";
    case "vaccine_stock_readiness":
    case "vaccine_cold_chain_check":
    case "vaccine_reorder_expiry_grn":
    case "pc_stock_anti_misuse":
      return "skip";
    default:
      return "drive";
  }
}

function vaccineLabel(event: CalendarEvent): string {
  const named = event.vaccine_name?.trim();
  if (named) return named;
  return event.title
    .replace(/^Preventive Care Vaccination Matrix\s+/i, "")
    .replace(/\s+catch-up drive$/i, "")
    .replace(/\s+due$/i, "")
    .trim();
}

function parkLabel(event: CalendarEvent): string {
  if (!genericScopeLabel(event.park_code)) return event.park_code!.trim();
  if (!genericScopeLabel(event.shed_name)) return event.shed_name!.trim();
  return "Park";
}

function summarizeLabels(labels: string[], fallback: string): string {
  if (labels.length === 0) return fallback;
  if (labels.length <= 3) return labels.join(" · ");
  return `${labels.slice(0, 3).join(" · ")} +${labels.length - 3} more`;
}

function aggregateCalendarEvents(events: CalendarEvent[]): CalendarDriveSummaryEvent[] {
  if (events.some((event) => Boolean((event as { aggregated?: boolean }).aggregated))) {
    return events as CalendarDriveSummaryEvent[];
  }
  const grouped = new Map<string, CalendarEvent[]>();
  for (const event of events) {
    const stage = driveStage(event);
    if (stage === "skip") continue;
    const bucket = [dateKey(event.due_at), event.owner_key || "pc", parkLabel(event), stage].join("|");
    const rows = grouped.get(bucket) ?? [];
    rows.push(event);
    grouped.set(bucket, rows);
  }
  return Array.from(grouped.entries())
    .map(([bucket, rows]) => {
      const first = rows[0];
      const stage = driveStage(first);
      const date = dateKey(first.due_at);
      const park = parkLabel(first);
      const sheds = uniqueStrings(rows.map((row) => row.shed_name));
      const vaccines = uniqueStrings(rows.map((row) => vaccineLabel(row)));
      const collapsedCount = rows.length;
      const scheduledCount = rows.filter((row) => ["scheduled", "due", "overdue", "missed", "in_progress", "proof_pending", "verification_pending", "rework_due"].includes(row.status)).length;
      const deferredCount = rows.filter((row) => row.status === "deferred").length;
      const reviewCount = stage === "review" ? collapsedCount : 0;
      const targetCount = rows.reduce((sum, row) => sum + row.target_count, 0);
      const summaryPrimary =
        stage === "review"
          ? `${collapsedCount} follow-up ${collapsedCount === 1 ? "item" : "items"} queued`
          : `${vaccines.length || collapsedCount} vaccine ${vaccines.length === 1 ? "queue" : "queues"}${sheds.length ? ` across ${sheds.length} shed${sheds.length === 1 ? "" : "s"}` : ""}`;
      const summarySecondary =
        stage === "review"
          ? summarizeLabels(vaccines, "Open to inspect review exceptions")
          : summarizeLabels(vaccines, "Open to inspect vaccine mix");
      const summaryTertiary =
        collapsedCount > 1 ? `${collapsedCount} loaded drive rows grouped into one card` : stage === "review" ? "Review items grouped by drive day" : "Grouped by drive day";
      return {
        ...first,
        event_id: `parkdrive:${bucket.replace(/\|/g, ":").replace(/\s+/g, "-").toLowerCase()}`,
        title: stage === "review" ? `${park} review queue` : `${park} vaccination drive`,
        subtitle: stage === "review" ? "Review queue" : "Park drive",
        target_count: targetCount,
        aggregated: true,
        all_day: true,
        summary_primary: summaryPrimary,
        summary_secondary: summarySecondary,
        summary_tertiary: summaryTertiary,
        shed_count: sheds.length,
        vaccine_count: vaccines.length,
        drive_count: collapsedCount,
        catch_up_count: rows.filter((row) => row.title.toLowerCase().includes("catch-up")).length,
        scheduled_count: scheduledCount,
        deferred_count: deferredCount,
        review_count: reviewCount,
        shed_labels: sheds,
        vaccine_labels: vaccines,
        due_at: `${date}T00:00:00+05:30`,
      } as CalendarDriveSummaryEvent;
    })
    .sort((a, b) => a.due_at.localeCompare(b.due_at) || a.title.localeCompare(b.title));
}

function EventRow({ event, href, ownerMeta, pageContract }: { event: CalendarEvent; href: string; ownerMeta: OwnerPresentationMap; pageContract: AdminUiPageContract }) {
  const summary = asDriveSummaryEvent(event);
  const meta = summary.aggregated
    ? [summary.subtitle, ownerLabel(event.owner_key, ownerMeta)]
        .filter(Boolean)
        .join(" · ")
    : [optionLabel(pageContract, "calendar_status", event.status), event.subtitle, event.park_code, event.shed_name, ownerLabel(event.owner_key, ownerMeta)]
    .filter(Boolean)
    .join(" · ");
  return (
    <Link href={href} replace scroll={false} className="ev celllink" style={{ borderLeftColor: ownerColor(event.owner_key, ownerMeta) }}>
      <div className="et">{summary.all_day ? "ALL DAY" : timeOf(event.due_at)}</div>
      <div className="eb">
        <b>{event.title}</b>
        <div className="em">{meta}</div>
        {summary.summary_primary ? (
          <div className="em" style={{ color: "var(--text)", fontWeight: 650 }}>
            {summary.summary_primary}
          </div>
        ) : null}
        {summary.summary_secondary ? <div className="em">{summary.summary_secondary}</div> : null}
      </div>
      {summary.aggregated ? <span className="erem">DRIVE</span> : null}
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
  const datePickerOpen = one(sp, "view") === "month";
  const requestedOwnerKey = (one(sp, "owner_key") || "all") as CalendarOwnerFilter;
  const selectedEventId = one(sp, "event");
  const groupedSelection = selectedEventId?.startsWith("parkdrive:") ?? false;
  const targetsCursor = one(sp, "targets_cursor");
  const targetsPage = boundedInt(one(sp, "targets_page"), 1, 1, 1000);
  const today = istToday();
  const dayFilter = one(sp, "day") || undefined;
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");

  // Agenda fetches from the selected as-of date; the compact date picker separately fetches the whole
  // selected month so drive days are highlighted even when the visible agenda window is empty.
  const anchorKey = asOf ? asOf.slice(0, 10) : today;
  const pickerWindow = monthWindow(anchorKey);
  const agendaWindow = weekWindow(anchorKey);

  const [list, pickerList, detail, targets] = await Promise.all([
    getCalendarVaccinationEvents({ parkId, ownerKey: requestedOwnerKey, dateFrom: agendaWindow.dateFrom, dateTo: agendaWindow.dateTo }),
    getCalendarVaccinationEvents({ parkId, ownerKey: requestedOwnerKey, dateFrom: pickerWindow.dateFrom, dateTo: pickerWindow.dateTo, includeDateMarkers: true }),
    selectedEventId && !groupedSelection ? getCalendarVaccinationEventDetail(selectedEventId) : Promise.resolve(null),
    selectedEventId && !groupedSelection ? getCalendarDriveTargets(selectedEventId, { cursor: targetsCursor, limit: 10 }) : Promise.resolve(null),
  ]);

  const rawEvents = list.ok ? list.data.items : [];
  const events = aggregateCalendarEvents(rawEvents);
  const rawPickerEvents = pickerList.ok ? pickerList.data.items : rawEvents;
  const pickerEvents = aggregateCalendarEvents(rawPickerEvents);
  if (list.ok && !list.data.presentation) {
    throw new Error(copy(pageContract, "error.presentation_missing"));
  }
  const presentation = list.ok && list.data.presentation ? list.data.presentation : fallbackCalendarPresentation(pageContract, requestedOwnerKey);
  const ownerMeta = ownerMetaFromPresentation(presentation);
  const activeOwnerKey = presentation.active_owner_key as CalendarOwnerFilter;

  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref(PATH, scope, {}, { view: datePickerOpen ? "month" : undefined, owner_key: activeOwnerKey === "all" ? undefined : activeOwnerKey, day: dayFilter, ...overrides });
  }
  const hrefForTab = (tab: CalendarPresentationTab | CalendarOwnerPresentationTab) => hrefWith({ ...presentationQueryToSearch(tab.query), event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const eventHref = (id: string) => hrefWith({ event: id, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const closeHref = hrefWith({ event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const weekHref = hrefWith({ view: undefined, event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const pickerMonthHref = (date: string) => hrefWith({ as_of: date, view: "month", day: undefined, event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const pickerDateHref = (date: string) => hrefWith({ as_of: date, view: undefined, day: undefined, event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });

  const sel = detail && detail.ok ? detail.data : null;
  const groupedSel = groupedSelection
    ? (events.find((event) => event.event_id === selectedEventId) as CalendarDriveSummaryEvent | undefined) ?? null
    : null;
  const spForTargets: RouteSearchParams = { ...sp, event: selectedEventId };
  function scopedTargetsHref(href: string | null): string | null {
    if (!href) return null;
    const [path, query = ""] = href.split("?");
    const extra = Object.fromEntries(new URLSearchParams(query));
    return scopeHref(path, scope, {}, extra);
  }
  const targetsNextHref =
    targets && targets.ok && targets.data.next_cursor
      ? scopedTargetsHref(hrefWithPagedCursor(PATH, spForTargets, "targets_cursor", targets.data.next_cursor, "targets_page", "targets_cursor_stack"))
      : null;
  const targetsPrevHref = scopedTargetsHref(hrefPreviousPagedCursor(PATH, spForTargets, "targets_cursor", "targets_page", "targets_cursor_stack"));
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
          <Link href={weekHref} replace scroll={false} className={datePickerOpen ? "" : "on"}>
            {optionLabel(pageContract, "calendar_view_tabs", "week")}
          </Link>
          <CalendarMonthPicker
            label={optionLabel(pageContract, "calendar_view_tabs", "month")}
            open={datePickerOpen}
            closeHref={weekHref}
          >
            <CalendarDatePicker
              events={pickerEvents}
              dateMarkers={pickerList.ok ? pickerList.data.date_markers : []}
              anchorKey={anchorKey}
              today={today}
              ownerMeta={ownerMeta}
              presentation={presentation}
              pageContract={pageContract}
              monthHref={pickerMonthHref}
              dateHref={pickerDateHref}
            />
          </CalendarMonthPicker>
        </div>
      </div>

      <div className="subtabs" style={{ margin: "0 0 10px" }} aria-label={copy(pageContract, "filter.owner.aria")}>
        {presentation.owner_tabs.map((tab) => (
          <Link
            key={tab.key}
            href={hrefForTab(tab)}
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
              <Link key={tab.key} href={hrefForTab(tab)} replace scroll={false} title={tab.disabled_reason || undefined}>
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
      {selectedEventId && !groupedSelection && detail && !detail.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{detail.error.code ?? detail.error.kind}</b>&nbsp;{detail.error.message}
        </div>
      ) : null}

      <WeekView
        events={events}
        today={today}
        eventHref={eventHref}
        ownerKey={activeOwnerKey}
        presentation={presentation}
        ownerMeta={ownerMeta}
        clearOwnerHref={hrefWith({ owner_key: undefined, event: undefined })}
        dayFilter={dayFilter}
        clearDayHref={hrefWith({ day: undefined, event: undefined })}
        pageContract={pageContract}
      />
      {events.length === 0 ? <EmptyState ok={list.ok} presentation={presentation} /> : null}

      {groupedSel ? (
        <CalendarDriveSummaryDrawer
          event={groupedSel}
          closeHref={closeHref}
          scope={scope}
          pageContract={pageContract}
        />
      ) : sel ? (
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

// ── Week (agenda) view ─────────────────────────────────────────────────────────────────────────────
function WeekView({
  events,
  today,
  eventHref,
  ownerKey,
  presentation,
  ownerMeta,
  clearOwnerHref,
  dayFilter,
  clearDayHref,
  pageContract,
}: {
  events: CalendarEvent[];
  today: string;
  eventHref: (id: string) => string;
  ownerKey: CalendarOwnerFilter;
  presentation: CalendarPresentation;
  ownerMeta: OwnerPresentationMap;
  clearOwnerHref: string;
  dayFilter?: string;
  clearDayHref: string;
  pageContract: AdminUiPageContract;
}) {
  const scoped = dayFilter ? events.filter((e) => weekdayOf(e.due_at) === dayFilter) : events;
  const sorted = [...scoped].sort((a, b) => a.due_at.localeCompare(b.due_at));
  const byDate = new Map<string, CalendarEvent[]>();
  for (const e of sorted) {
    const key = dateKey(e.due_at);
    const bucket = byDate.get(key) ?? [];
    bucket.push(e);
    byDate.set(key, bucket);
  }
  const selectedOwnerLabel = ownerScopeLabel(ownerKey, ownerMeta);
  const showFilteredEmpty = byDate.size === 0 && events.length > 0;

  return (
    <div className="card">
      <div className="hd">
        <CalendarDays className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{presentation.week.title}</h3>
        <Tag tone={ownerKey === "all" ? "mut" : "info"}>{selectedOwnerLabel}</Tag>
        {dayFilter ? <Tag tone="info">{dayFilter}</Tag> : null}
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
        <div className="fchipsbar calband">
          {dayFilter ? (
            <>
              {copy(pageContract, "week.showing_prefix")} <b>{dayFilter}</b> {copy(pageContract, "week.only_suffix")}
              <Link href={clearDayHref} replace scroll={false} className="lenslink">
                ↺ {presentation.week.clear_day_label}
              </Link>
            </>
          ) : (
            <>
              {presentation.week.whole_period_message}
              <span className="lenslink mutedlink" aria-disabled="true">
                {presentation.week.all_days_selected_label}
              </span>
            </>
          )}
        </div>
        <div className="agenda">
          {byDate.size === 0 && showFilteredEmpty ? (
            <p className="muted small" style={{ margin: "4px 2px" }}>
              {presentation.week.empty_message}
              {dayFilter ? ` ${copy(pageContract, "week.empty_day_prefix")} ${dayFilter}` : ""} {copy(pageContract, "week.empty_scope_suffix")}
            </p>
          ) : byDate.size > 0 ? (
            Array.from(byDate.entries()).map(([key, rows]) => (
              <div key={key}>
                <div className="dh">{dateHeading(key, today, pageContract)}</div>
                {rows.map((e) => (
                  <EventRow key={e.event_id} event={e} href={eventHref(e.event_id)} ownerMeta={ownerMeta} pageContract={pageContract} />
                ))}
              </div>
            ))
          ) : null}
        </div>
      </div>
    </div>
  );
}

// ── Compact month picker ───────────────────────────────────────────────────────────────────────────
function CalendarDatePicker({
  events,
  dateMarkers,
  anchorKey,
  today,
  ownerMeta,
  presentation,
  pageContract,
  monthHref,
  dateHref,
}: {
  events: CalendarEvent[];
  dateMarkers: CalendarDateMarker[];
  anchorKey: string;
  today: string;
  ownerMeta: OwnerPresentationMap;
  presentation: CalendarPresentation;
  pageContract: AdminUiPageContract;
  monthHref: (date: string) => string;
  dateHref: (date: string) => string;
}) {
  const anchor = new Date(`${anchorKey}T00:00:00+05:30`);
  const year = Number.isNaN(anchor.getTime()) ? Number(today.slice(0, 4)) : anchor.getFullYear();
  const month = Number.isNaN(anchor.getTime()) ? Number(today.slice(5, 7)) - 1 : anchor.getMonth();
  const weekdays = optionGroup(pageContract, "calendar_weekdays");

  const byDate = new Map<string, CalendarEvent[]>();
  for (const e of events) {
    const key = dateKey(e.due_at);
    const bucket = byDate.get(key) ?? [];
    bucket.push(e);
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
  const previous = new Date(year, month - 1, 1);
  const next = new Date(year, month + 1, 1);
  const previousMonth = `${previous.getFullYear()}-${String(previous.getMonth() + 1).padStart(2, "0")}-01`;
  const nextMonth = `${next.getFullYear()}-${String(next.getMonth() + 1).padStart(2, "0")}-01`;

  return (
    <div className="calpicker-popover">
      <div className="calpicker-head">
        <Link href={monthHref(previousMonth)} replace scroll={false} className="btn icon" aria-label={copy(pageContract, "calendar.picker.previous_month")}>
          <ChevronLeft className="ic" aria-hidden="true" />
        </Link>
        <b>{`${optionLabel(pageContract, "calendar_months", String(month))} ${year}`}</b>
        <Link href={monthHref(nextMonth)} replace scroll={false} className="btn icon" aria-label={copy(pageContract, "calendar.picker.next_month")}>
          <ChevronRight className="ic" aria-hidden="true" />
        </Link>
      </div>
      <div className="calpicker-grid">
        {weekdays.map((d) => (
          <span key={d.key} className="calpicker-dow">
            {d.label.slice(0, 3)}
          </span>
        ))}
        {cells.map((cell, i) => {
          if (cell === null) return <span key={`out-${i}`} className="calpicker-day out" />;
          const dayEvents = byDate.get(cell.key) ?? [];
          const marker = markerByDate.get(cell.key);
          const hasDrive = dayEvents.length > 0;
          const hasHistory = (marker?.completed_count ?? 0) > 0;
          const hasOpenWork = (marker?.open_count ?? 0) > 0;
          const owners = Array.from(new Set(dayEvents.map((e) => e.owner_key))).slice(0, 3);
          const eventCount = marker?.event_count ?? dayEvents.length;
          return (
            <Link
              key={cell.key}
              href={dateHref(cell.key)}
              replace
              scroll={false}
              className={`calpicker-day${cell.key === today ? " today" : ""}${cell.key === anchorKey ? " selected" : ""}${eventCount ? " has-events" : ""}${hasDrive ? " has-drive" : ""}${hasHistory ? " has-history" : ""}${hasOpenWork ? " has-open-work" : ""}`}
              title={
                dayEvents.length
                  ? dayEvents
                      .slice(0, 4)
                      .map((e) => `${asDriveSummaryEvent(e).all_day ? "All day" : timeOf(e.due_at)} ${eventTypeMeta(e.event_type, presentation).label} - ${e.title}`)
                      .join("\n")
                  : undefined
              }
            >
              <span>{cell.day}</span>
              {owners.length ? (
                <i>
                  {owners.map((owner) => (
                    <em key={owner} style={{ background: ownerColor(owner, ownerMeta) }} />
                  ))}
                </i>
              ) : null}
            </Link>
          );
        })}
      </div>
      <div className="calpicker-note">
        <span className="calpicker-dot drive" /> Drive day
        <span className="sp" />
        <span>{presentation.month.as_of_hint}</span>
      </div>
    </div>
  );
}

function EmptyState({ ok, presentation }: { ok: boolean; presentation: CalendarPresentation }) {
  return (
    <div className="calempty" role="status">
      <span className="calempty-ic">
        {ok ? <CalendarDays className="ic" aria-hidden="true" /> : <Info className="ic" aria-hidden="true" />}
      </span>
      <div className="calempty-copy">
        <b>{ok ? presentation.week.empty_message : presentation.empty_state.error_message}</b>
        <span>{ok ? presentation.empty_state.ok_message : presentation.empty_state.error_message}</span>
      </div>
      {ok ? <Tag tone="mut">{presentation.active_owner_scope_label}</Tag> : null}
    </div>
  );
}
