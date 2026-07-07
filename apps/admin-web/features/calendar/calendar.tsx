import Link from "next/link";
import { CalendarDays, ChevronLeft, ChevronRight, Clock, Info, Plus } from "lucide-react";
import { actionFeedbackCopy, copy, optionGroup, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, hrefWithoutAction, hrefWithPagedCursor, hrefPreviousPagedCursor, boundedInt, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref, type Scope } from "@/lib/scope";
import { fmtDate as fmtIstDate, todayIso } from "@/lib/format";
import { Tag } from "@/components/ui-primitives";
import {
  activeEscalationState,
  activeReminderState,
  eventTypeMeta,
  fallbackCalendarPresentation,
  hasReminderOrEscalation,
  ownerColor,
  ownerLabel,
  ownerMetaFromPresentation,
  ownerScopeLabel,
  presentationQueryToSearch,
  type CalendarEvent,
  type CalendarOwnerFilter,
  type CalendarOwnerPresentationTab,
  type CalendarPresentation,
  type CalendarPresentationTab,
  type OwnerPresentationMap,
} from "./calendar-contract";
import { getCalendarVaccinationEvents, getCalendarVaccinationEventDetail, getCalendarDriveTargets } from "./calendar-server";
import { CalendarEventDrawer } from "./calendar-event-drawer";

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

// First/last calendar day of the month an anchor date (YYYY-MM-DD) falls in. Deterministic (args-only Date).
function monthWindow(anchorKey: string): { dateFrom: string; dateTo: string } {
  const year = Number(anchorKey.slice(0, 4));
  const month = Number(anchorKey.slice(5, 7)); // 1-based
  const lastDay = new Date(year, month, 0).getDate(); // day 0 of next month = last day of this month
  const ym = anchorKey.slice(0, 7);
  return { dateFrom: `${ym}-01`, dateTo: `${ym}-${String(lastDay).padStart(2, "0")}` };
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

function EventRow({ event, href, ownerMeta, pageContract }: { event: CalendarEvent; href: string; ownerMeta: OwnerPresentationMap; pageContract: AdminUiPageContract }) {
  const meta = [optionLabel(pageContract, "calendar_status", event.status), event.subtitle, event.park_code, event.shed_name, ownerLabel(event.owner_key, ownerMeta)]
    .filter(Boolean)
    .join(" · ");
  const escalationState = activeEscalationState(event);
  const reminderState = activeReminderState(event);
  const badgeGroup = escalationState ? "calendar_escalation_state" : "calendar_reminder_state";
  const badgeState = escalationState || reminderState;
  const badge = badgeState ? optionLabel(pageContract, badgeGroup, badgeState) : "";
  return (
    <Link href={href} replace scroll={false} className="ev celllink" style={{ borderLeftColor: ownerColor(event.owner_key, ownerMeta) }}>
      <div className="et">{timeOf(event.due_at)}</div>
      <div className="eb">
        <b>{event.title}</b>
        <div className="em">{meta}</div>
      </div>
      {badge ? <span className="erem">{badge}</span> : null}
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
  const view = one(sp, "view") === "month" ? "month" : "week";
  const requestedOwnerKey = (one(sp, "owner_key") || "all") as CalendarOwnerFilter;
  const selectedEventId = one(sp, "event");
  const targetsCursor = one(sp, "targets_cursor");
  const targetsPage = boundedInt(one(sp, "targets_page"), 1, 1, 1000);
  const today = istToday();
  const dayFilter = one(sp, "day") || undefined;
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");

  // Bound the list window to what the view actually renders. Month view must fetch the WHOLE anchor month
  // (else early/late days render empty); week view fetches from the as-of date and lets the backend default
  // date_to to +30d. A calendar month is ≤31 days, well under the API's 45-day max.
  const anchorKey = asOf ? asOf.slice(0, 10) : today;
  const { dateFrom, dateTo } = view === "month" ? monthWindow(anchorKey) : { dateFrom: asOf ? asOf.slice(0, 10) : undefined, dateTo: undefined };

  const [list, detail, targets] = await Promise.all([
    getCalendarVaccinationEvents({ parkId, ownerKey: requestedOwnerKey, dateFrom, dateTo }),
    selectedEventId ? getCalendarVaccinationEventDetail(selectedEventId) : Promise.resolve(null),
    selectedEventId ? getCalendarDriveTargets(selectedEventId, { cursor: targetsCursor, limit: 10 }) : Promise.resolve(null),
  ]);

  const events = list.ok ? list.data.items : [];
  if (list.ok && !list.data.presentation) {
    throw new Error(copy(pageContract, "error.presentation_missing"));
  }
  const presentation = list.ok && list.data.presentation ? list.data.presentation : fallbackCalendarPresentation(pageContract, requestedOwnerKey);
  const ownerMeta = ownerMetaFromPresentation(presentation);
  const activeOwnerKey = presentation.active_owner_key as CalendarOwnerFilter;

  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref(PATH, scope, {}, { view: view === "month" ? "month" : undefined, owner_key: activeOwnerKey === "all" ? undefined : activeOwnerKey, day: dayFilter, ...overrides });
  }
  const hrefForTab = (tab: CalendarPresentationTab | CalendarOwnerPresentationTab) => hrefWith({ ...presentationQueryToSearch(tab.query), event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const eventHref = (id: string) => hrefWith({ event: id, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const closeHref = hrefWith({ event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });

  const sel = detail && detail.ok ? detail.data : null;
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
	          <h1>{pageContract.title || presentation.page_title}</h1>
	          <div className="sub">{pageContract.subtitle || presentation.page_subtitle}</div>
	        </div>
	      </div>

      <div style={{ display: "flex", gap: 10, alignItems: "center", marginBottom: 14, flexWrap: "wrap" }}>
        <div className="subtabs" style={{ margin: 0 }}>
          {presentation.view_tabs.map((tab) => (
            <Link
              key={tab.key}
              href={hrefForTab(tab)}
              replace
              scroll={false}
              className={view === tab.key ? "on" : tab.enabled ? "" : "disabled"}
              aria-disabled={tab.enabled ? undefined : "true"}
              title={tab.enabled ? undefined : tab.disabled_reason}
            >
              {tab.label}
            </Link>
          ))}
        </div>
        <button type="button" className="btn p" aria-disabled={presentation.new_event.enabled ? undefined : "true"} title={presentation.new_event.disabled_reason}>
          <Plus className="ic" aria-hidden="true" />
          {presentation.new_event.label}
        </button>
      </div>

	      <div className="subtabs" style={{ margin: "0 0 6px" }} aria-label={copy(pageContract, "filter.owner.aria")}>
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

	      <div className="subtabs" style={{ margin: "0 0 14px" }} aria-label={copy(pageContract, "filter.workstream.aria")}>
        {presentation.workstream_tabs.map((tab) =>
          tab.enabled && !tab.active ? (
            <Link key={tab.key} href={hrefForTab(tab)} replace scroll={false} title={tab.disabled_reason || undefined}>
              {tab.label}
            </Link>
          ) : (
            <button key={tab.key} type="button" className={tab.active ? "on" : "disabled"} aria-current={tab.active ? "true" : undefined} aria-disabled={tab.enabled ? undefined : "true"} title={tab.disabled_reason || undefined}>
              {tab.label}
            </button>
          ),
        )}
      </div>

      <div className="card" style={{ marginBottom: 16 }}>
        <div className="bd">
          <div className="b700" style={{ marginBottom: 9 }}>
            {presentation.rhythm.title} <span className="muted small">— {presentation.rhythm.note}</span>
          </div>
          <div className="rhythm">
            {presentation.rhythm.days.map((day) => {
              const dayQuery = presentationQueryToSearch(day.query);
              const targetDay = dayQuery.day ?? day.day;
              const selected = dayFilter === targetDay;
              const included = !dayFilter;
              return (
                <Link
                  key={day.day}
                  href={hrefWith({ ...dayQuery, day: selected ? undefined : targetDay, event: undefined })}
                  replace
                  scroll={false}
                  className={`rday ${selected ? "today" : included ? "included" : ""}`}
                  aria-label={included ? `${day.day} is included in whole-week vaccination work` : `Show ${day.day} vaccination work: ${day.label.toLowerCase()}`}
                  aria-disabled={day.enabled ? undefined : "true"}
                >
                  <div className="d">{day.day}</div>
                  <div className={`mode ${day.tone}`}>{day.label}</div>
                </Link>
              );
            })}
          </div>
        </div>
      </div>

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

      {events.length === 0 ? (
        <EmptyState scope={scope} ok={list.ok} presentation={presentation} />
      ) : view === "month" ? (
        <MonthView events={events} asOf={asOf} today={today} eventHref={eventHref} ownerKey={activeOwnerKey} presentation={presentation} ownerMeta={ownerMeta} pageContract={pageContract} />
      ) : (
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
      )}

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
  // Rhythm day filter: when a weekday is selected, show only that weekday's due work.
  const scoped = dayFilter ? events.filter((e) => weekdayOf(e.due_at) === dayFilter) : events;
  const sorted = [...scoped].sort((a, b) => a.due_at.localeCompare(b.due_at));
  const byDate = new Map<string, CalendarEvent[]>();
  for (const e of sorted) {
    const key = dateKey(e.due_at);
    const bucket = byDate.get(key) ?? [];
    bucket.push(e);
    byDate.set(key, bucket);
  }
  const remindable = sorted.filter(hasReminderOrEscalation);
  const selectedOwnerLabel = ownerScopeLabel(ownerKey, ownerMeta);
  const legendOwners = ownerKey === "all" ? presentation.owner_tabs.filter((tab) => tab.key !== "all") : presentation.owner_tabs.filter((tab) => tab.key === ownerKey);

  return (
    <div className="grid calendar-week-grid">
      <div className="card">
        <div className="hd">
          <CalendarDays className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{presentation.week.title}</h3>
          <Tag tone={ownerKey === "all" ? "mut" : "info"}>{selectedOwnerLabel}</Tag>
          {dayFilter ? <Tag tone="info">{dayFilter}</Tag> : null}
          <div className="sp" style={{ flex: 1 }} />
          <span className="legend" aria-label={ownerKey === "all" ? copy(pageContract, "week.legend.all_owner_lanes") : `${selectedOwnerLabel} ${copy(pageContract, "week.legend.lane_suffix")}`}>
            {legendOwners.map((owner) => (
              <span key={owner.key}>
                <span className="sw" style={{ background: owner.color }} /> {owner.label}
              </span>
            ))}
          </span>
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
                {presentation.week.whole_period_message || (
                  <>
                    {copy(pageContract, "week.showing_prefix")} <b>{copy(pageContract, "week.whole_week")}</b>.
                  </>
                )}
                <span className="lenslink mutedlink" aria-disabled="true">
                  {presentation.week.all_days_selected_label}
                </span>
              </>
            )}
          </div>
          <div className="agenda">
            {byDate.size === 0 ? (
              <p className="muted small" style={{ margin: "4px 2px" }}>
                {presentation.week.empty_message}
                {dayFilter ? ` ${copy(pageContract, "week.empty_day_prefix")} ${dayFilter}` : ""} {copy(pageContract, "week.empty_scope_suffix")}
              </p>
            ) : (
              Array.from(byDate.entries()).map(([key, rows]) => (
                <div key={key}>
                  <div className="dh">{dateHeading(key, today, pageContract)}</div>
                  {rows.map((e) => (
                    <EventRow key={e.event_id} event={e} href={eventHref(e.event_id)} ownerMeta={ownerMeta} pageContract={pageContract} />
                  ))}
                </div>
              ))
            )}
          </div>
        </div>
      </div>

      <div className="card">
        <div className="hd">
          <Clock className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />
          <h3>{presentation.week.reminder_title}</h3>
        </div>
        <div className="bd feed">
          {remindable.length === 0 ? (
            <p className="muted small" style={{ margin: 0 }}>
              {presentation.week.reminder_empty_message}
            </p>
          ) : (
            remindable.map((e) => {
              const escalationState = activeEscalationState(e);
              const reminderState = activeReminderState(e);
              const visibleReminderState = reminderState === "escalated" && escalationState ? "" : reminderState;
              return (
                <Link key={e.event_id} href={eventHref(e.event_id)} replace scroll={false} className="fitem">
                  <span className="fic" style={{ background: `color-mix(in srgb, ${ownerColor(e.owner_key, ownerMeta)} 20%, var(--panel))`, color: ownerColor(e.owner_key, ownerMeta) }}>
                    <Clock className="ic" aria-hidden="true" />
                  </span>
                  <div className="tx">
                    <b>{e.title}</b>
                    <div className="mt">{[ownerLabel(e.owner_key, ownerMeta), e.park_code, e.shed_name].filter(Boolean).join(" · ")}</div>
                    <div style={{ marginTop: 4, display: "flex", gap: 4, flexWrap: "wrap" }}>
                      <span className="tag t-info" style={{ fontSize: 10 }}>
                        {e.primary_notification_channel || copy(pageContract, "label.not_configured")}
                      </span>
                      {escalationState ? (
                        <span className={`tag t-${optionTone(pageContract, "calendar_escalation_state", escalationState)}`} style={{ fontSize: 10 }}>
                          {optionLabel(pageContract, "calendar_escalation_state", escalationState)}
                        </span>
                      ) : null}
                      {visibleReminderState ? (
                        <span className={`tag t-${optionTone(pageContract, "calendar_reminder_state", visibleReminderState)}`} style={{ fontSize: 10 }}>
                          {optionLabel(pageContract, "calendar_reminder_state", visibleReminderState)}
                        </span>
                      ) : null}
                    </div>
                  </div>
                </Link>
              );
            })
          )}
          <div className="note" style={{ marginTop: 10 }}>
            {presentation.week.reminder_note}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Month view — mock .mcal grid ────────────────────────────────────────────────────────────────────
function MonthView({
  events,
  asOf,
  today,
  eventHref,
  ownerKey,
  presentation,
  ownerMeta,
  pageContract,
}: {
  events: CalendarEvent[];
  asOf?: string;
  today: string;
  eventHref: (id: string) => string;
  ownerKey: CalendarOwnerFilter;
  presentation: CalendarPresentation;
  ownerMeta: OwnerPresentationMap;
  pageContract: AdminUiPageContract;
}) {
  const anchorKey = asOf ? asOf.slice(0, 10) : today;
  const anchor = new Date(`${anchorKey}T00:00:00+05:30`);
  const year = Number.isNaN(anchor.getTime()) ? Number(today.slice(0, 4)) : anchor.getFullYear();
  const month = Number.isNaN(anchor.getTime()) ? Number(today.slice(5, 7)) - 1 : anchor.getMonth();
  const selectedOwnerLabel = ownerScopeLabel(ownerKey, ownerMeta);
  const weekdays = optionGroup(pageContract, "calendar_weekdays");

  const byDate = new Map<string, CalendarEvent[]>();
  for (const e of events) {
    const key = dateKey(e.due_at);
    const bucket = byDate.get(key) ?? [];
    bucket.push(e);
    byDate.set(key, bucket);
  }

  const firstDow = new Date(year, month, 1).getDay();
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const cells: ({ day: number; key: string } | null)[] = [];
  for (let i = 0; i < firstDow; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) {
    cells.push({ day: d, key: `${year}-${String(month + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}` });
  }

  return (
    <div className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <CalendarDays className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{`${optionLabel(pageContract, "calendar_months", String(month))} ${year}`}</h3>
        <Tag tone={ownerKey === "all" ? "mut" : "info"}>{selectedOwnerLabel}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">
          <ChevronLeft className="ic" style={{ width: 13, verticalAlign: "middle" }} aria-hidden="true" />
          {presentation.month.as_of_hint}
          <ChevronRight className="ic" style={{ width: 13, verticalAlign: "middle" }} aria-hidden="true" />
        </span>
      </div>
      <div className="bd">
        <div className="mcal">
          {weekdays.map((d) => (
            <div key={d.key} className="mh">
              {d.label}
            </div>
          ))}
          {cells.map((cell, i) => {
            if (cell === null) return <div key={`out-${i}`} className="mcell out" />;
            const dayEvents = byDate.get(cell.key) ?? [];
            return (
              <div key={cell.key} className={`mcell ${cell.key === today ? "today" : ""}`}>
                <div className="dn">{cell.day}</div>
                {dayEvents.slice(0, 3).map((e) => (
                  <Link
                    key={e.event_id}
                    href={eventHref(e.event_id)}
                    replace
                    scroll={false}
                    className="mev"
                    style={{ background: `color-mix(in srgb, ${ownerColor(e.owner_key, ownerMeta)} 16%, var(--panel))`, borderLeftColor: ownerColor(e.owner_key, ownerMeta) }}
                    title={`${e.title} · ${optionLabel(pageContract, "calendar_status", e.status)}`}
                  >
                    {eventTypeMeta(e.event_type, presentation).label}
                  </Link>
                ))}
                {dayEvents.length > 3 ? (
                  <div className="muted" style={{ fontSize: 9 }}>
                    +{dayEvents.length - 3} {copy(pageContract, "label.more")}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
        <div className="note" style={{ marginTop: 10 }}>
          {presentation.month.cell_note}
        </div>
      </div>
    </div>
  );
}

function EmptyState({ scope, ok, presentation }: { scope: Scope; ok: boolean; presentation: CalendarPresentation }) {
  return (
    <div className="note" style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
      <Info className="ic" aria-hidden="true" style={{ color: "var(--brand)", flexShrink: 0 }} />
      <span>{ok ? presentation.empty_state.ok_message : presentation.empty_state.error_message}</span>
      {ok ? (
        <>
          <Link href={scopeHref("/config", scope, {}, { category: "vaccination" })} className="btn sm">
            {presentation.empty_state.primary_label}
          </Link>
          <Link href={scopeHref("/vaccination", scope)} className="btn sm">
            {presentation.empty_state.secondary_label}
          </Link>
        </>
      ) : null}
    </div>
  );
}
