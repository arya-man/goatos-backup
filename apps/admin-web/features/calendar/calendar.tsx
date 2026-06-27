import Link from "next/link";
import { CalendarDays, ChevronLeft, ChevronRight, Clock, Info, Plus } from "lucide-react";
import { one, hrefWithoutAction, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref, type Scope } from "@/lib/scope";
import { Tag } from "@/components/ui-primitives";
import {
  eventTypeMeta,
  fallbackCalendarPresentation,
  hasReminderOrEscalation,
  ownerColor,
  ownerLabel,
  ownerMetaFromPresentation,
  ownerScopeLabel,
  presentationQueryToSearch,
  rowReminderBadge,
  statusMeta,
  type CalendarEvent,
  type CalendarOwnerFilter,
  type CalendarOwnerPresentationTab,
  type CalendarPresentation,
  type CalendarPresentationTab,
  type OwnerPresentationMap,
} from "./calendar-contract";
import { getCalendarVaccinationEvents, getCalendarVaccinationEventDetail } from "./calendar-server";
import { CalendarEventDrawer } from "./calendar-event-drawer";
import { CalendarBackButton } from "./calendar-back";

type CalendarOriginCrumb = { label: string; href: string; params?: Record<string, string | undefined> };

// Optional origin trail: a linking screen can pass ?from=<key> so the breadcrumb shows where Calendar was
// opened from. Direct sidebar nav omits `from`, so the row shows only Back + Calendar. The crumb is a hint,
// not the back mechanism; the Back button still uses real browser history.
const FROM_MAP: Record<string, CalendarOriginCrumb[]> = {
  "control-tower": [{ label: "Control Tower", href: "/" }],
  "action-center": [{ label: "Action Center", href: "/action-center" }],
  workflows: [{ label: "Workflows", href: "/workflows" }],
  "protocol-adherence": [{ label: "Protocol Adherence", href: "/protocol-adherence" }],
  vaccination: [{ label: "Vaccination", href: "/vaccination" }],
  "medicines-vaccines": [
    { label: "Control Tower", href: "/" },
    { label: "Calendar", href: "/calendar" },
    { label: "Medicines & vaccines", href: "/calendar", params: { owner_key: "inventory" } },
  ],
  "inventory-medicines-vaccines": [
    { label: "Control Tower", href: "/" },
    { label: "Calendar", href: "/calendar" },
    { label: "Medicines & vaccines", href: "/calendar", params: { owner_key: "inventory" } },
  ],
};

const PATH = "/calendar";
const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

// IST weekday short name for an arbitrary event instant (for the day filter).
function weekdayOf(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const wd = new Intl.DateTimeFormat("en-US", { timeZone: "Asia/Kolkata", weekday: "short" }).format(d);
  return wd; // "Mon".."Sun"
}

// Business "today" in the operating-tenant timezone (IST). Used for the month highlight + agenda "TODAY".
function istToday(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata" }).format(new Date());
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
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata" }).format(d);
}

function dateHeading(key: string, today: string): string {
  const d = new Date(`${key}T00:00:00+05:30`);
  if (Number.isNaN(d.getTime())) return key;
  const wd = WEEKDAYS[d.getDay()].toUpperCase();
  if (key === today) return `${wd} · TODAY`;
  return `${wd} · ${MONTHS[d.getMonth()].slice(0, 3).toUpperCase()} ${d.getDate()}`;
}

function EventRow({ event, href, ownerMeta }: { event: CalendarEvent; href: string; ownerMeta: OwnerPresentationMap }) {
  const meta = [statusMeta(event.status).label, event.subtitle, event.park_code, event.shed_name, ownerLabel(event.owner_key, ownerMeta)]
    .filter(Boolean)
    .join(" · ");
  const badge = rowReminderBadge(event);
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

export async function VaccinationCalendarPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const view = one(sp, "view") === "month" ? "month" : "week";
  const requestedOwnerKey = (one(sp, "owner_key") || "all") as CalendarOwnerFilter;
  const selectedEventId = one(sp, "event");
  const today = istToday();
  const dayFilter = one(sp, "day") || undefined;
  const fromKey = one(sp, "from");
  const originTrail = fromKey ? FROM_MAP[fromKey] : undefined;
  const actionStatus = one(sp, "action_status");
  const actionMessage = one(sp, "action_message");

  // Bound the list window to what the view actually renders. Month view must fetch the WHOLE anchor month
  // (else early/late days render empty); week view fetches from the as-of date and lets the backend default
  // date_to to +30d. A calendar month is ≤31 days, well under the API's 45-day max.
  const anchorKey = asOf ? asOf.slice(0, 10) : today;
  const { dateFrom, dateTo } = view === "month" ? monthWindow(anchorKey) : { dateFrom: asOf ? asOf.slice(0, 10) : undefined, dateTo: undefined };

  const [list, detail] = await Promise.all([
    getCalendarVaccinationEvents({ parkId, ownerKey: requestedOwnerKey, dateFrom, dateTo }),
    selectedEventId ? getCalendarVaccinationEventDetail(selectedEventId) : Promise.resolve(null),
  ]);

  const events = list.ok ? list.data.items : [];
  const presentation = list.ok && list.data.presentation ? list.data.presentation : fallbackCalendarPresentation(requestedOwnerKey);
  const ownerMeta = ownerMetaFromPresentation(presentation);
  const activeOwnerKey = presentation.active_owner_key as CalendarOwnerFilter;

  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref(PATH, scope, {}, { view: view === "month" ? "month" : undefined, owner_key: activeOwnerKey === "all" ? undefined : activeOwnerKey, day: dayFilter, ...overrides });
  }
  const hrefForTab = (tab: CalendarPresentationTab | CalendarOwnerPresentationTab) => hrefWith({ ...presentationQueryToSearch(tab.query), event: undefined });
  const eventHref = (id: string) => hrefWith({ event: id });
  const closeHref = hrefWith({ event: undefined });

  const sel = detail && detail.ok ? detail.data : null;

  return (
    <div className="screen on">
      <div className="navback">
        <CalendarBackButton />
        {originTrail ? (
          <div className="nbtrail">
            {originTrail.map((crumb, index) => (
              <span key={`${crumb.label}:${index}`} style={{ display: "contents" }}>
                {index > 0 ? <ChevronRight className="ic nbsep" aria-hidden="true" /> : null}
                <Link href={scopeHref(crumb.href, scope, {}, crumb.params ?? {})} className="nbc">
                  {crumb.label}
                </Link>
              </span>
            ))}
            <ChevronRight className="ic nbsep" aria-hidden="true" />
            <span className="nbc cur">{presentation.page_title}</span>
          </div>
        ) : (
          <span className="nbtitle">{presentation.page_title}</span>
        )}
      </div>
      <div className="phead">
        <div>
          <h1>{presentation.page_title}</h1>
          <div className="sub">{presentation.page_subtitle}</div>
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

      <div className="subtabs" style={{ margin: "0 0 6px" }} aria-label="Owner">
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

      <div className="subtabs" style={{ margin: "0 0 14px" }} aria-label="Calendar workstream">
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
            <Tag tone="ok">done</Tag> {actionMessage ?? "Action completed."}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
            <b>Action failed</b>&nbsp;{actionMessage ?? actionStatus}
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
        <MonthView events={events} asOf={asOf} today={today} eventHref={eventHref} ownerKey={activeOwnerKey} presentation={presentation} ownerMeta={ownerMeta} />
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
        />
      )}

      {sel ? <CalendarEventDrawer detail={sel} closeHref={closeHref} returnTo={hrefWithoutAction(PATH, sp)} scope={scope} presentation={presentation} ownerMeta={ownerMeta} /> : null}
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
          <span className="legend" aria-label={ownerKey === "all" ? "All owner lanes" : `${selectedOwnerLabel} lane`}>
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
                  Showing <b>{selectedOwnerLabel}</b> work only.
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
                Showing <b>{dayFilter}</b> only.
                <Link href={clearDayHref} replace scroll={false} className="lenslink">
                  ↺ {presentation.week.clear_day_label}
                </Link>
              </>
            ) : (
              <>
                {presentation.week.whole_period_message || (
                  <>
                    Showing <b>whole week</b>.
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
                {dayFilter ? ` on ${dayFilter}` : ""} in this scope.
              </p>
            ) : (
              Array.from(byDate.entries()).map(([key, rows]) => (
                <div key={key}>
                  <div className="dh">{dateHeading(key, today)}</div>
                  {rows.map((e) => (
                    <EventRow key={e.event_id} event={e} href={eventHref(e.event_id)} ownerMeta={ownerMeta} />
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
            remindable.map((e) => (
              <Link key={e.event_id} href={eventHref(e.event_id)} replace scroll={false} className="fitem">
                <span className="fic" style={{ background: `color-mix(in srgb, ${ownerColor(e.owner_key, ownerMeta)} 20%, var(--panel))`, color: ownerColor(e.owner_key, ownerMeta) }}>
                  <Clock className="ic" aria-hidden="true" />
                </span>
                <div className="tx">
                  <b>{e.title}</b>
                  <div className="mt">{[ownerLabel(e.owner_key, ownerMeta), e.park_code, e.shed_name].filter(Boolean).join(" · ")}</div>
                  <div style={{ marginTop: 4, display: "flex", gap: 4, flexWrap: "wrap" }}>
                    <span className="tag t-info" style={{ fontSize: 10 }}>
                      {e.primary_notification_channel || "channel not configured"}
                    </span>
                    {e.escalation_state === "pending" || e.escalation_state === "escalated" ? (
                      <span className="tag t-dng" style={{ fontSize: 10 }}>
                        {e.escalation_state === "pending" ? "escalation" : "escalated"}
                      </span>
                    ) : null}
                    {e.reminder_state === "snoozed" ? (
                      <span className="tag t-pur" style={{ fontSize: 10 }}>
                        snoozed
                      </span>
                    ) : e.reminder_state === "nudged" ? (
                      <span className="tag t-info" style={{ fontSize: 10 }}>
                        nudged
                      </span>
                    ) : null}
                  </div>
                </div>
              </Link>
            ))
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
}: {
  events: CalendarEvent[];
  asOf?: string;
  today: string;
  eventHref: (id: string) => string;
  ownerKey: CalendarOwnerFilter;
  presentation: CalendarPresentation;
  ownerMeta: OwnerPresentationMap;
}) {
  const anchorKey = asOf ? asOf.slice(0, 10) : today;
  const anchor = new Date(`${anchorKey}T00:00:00+05:30`);
  const year = Number.isNaN(anchor.getTime()) ? Number(today.slice(0, 4)) : anchor.getFullYear();
  const month = Number.isNaN(anchor.getTime()) ? Number(today.slice(5, 7)) - 1 : anchor.getMonth();
  const selectedOwnerLabel = ownerScopeLabel(ownerKey, ownerMeta);

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
        <h3>{`${MONTHS[month]} ${year}`}</h3>
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
          {WEEKDAYS.map((d) => (
            <div key={d} className="mh">
              {d}
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
                    title={`${e.title} · ${statusMeta(e.status).label}`}
                  >
                    {eventTypeMeta(e.event_type, presentation).label}
                  </Link>
                ))}
                {dayEvents.length > 3 ? (
                  <div className="muted" style={{ fontSize: 9 }}>
                    +{dayEvents.length - 3} more
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
