// Week-view side panels ported from the ops-console calendar mock's anatomy (the
// `#weekView` two-column grid + weekly rhythm card): the owner-color legend, the reminders &
// escalation rail (`.feed`/`.fitem`), and the weekly operating-rhythm strip (`.rhythm`/`.rday`). All
// three read real backend-compiled contract data (`CalendarPresentation.rhythm` / `.week.reminder_*`,
// already served by `/admin-web/bootstrap` — see backend/internal/adminui/app/service.go) rather than
// inventing local copy or the mock's fixed multi-channel sample data.
import Link from "next/link";
import { Bell, Clock } from "lucide-react";
import { Tag } from "@/components/ui-primitives";
import {
  type CalendarPresentation,
  type CalendarReminderRail,
  type CalendarRhythmDay,
  type OwnerPresentationMap,
} from "./calendar-contract";

// Real owner-color key, ported from the mock's `.legend`/`.sw` swatch row. The mock's sample legend
// (shed-scoped / fodder-non-shed / coordination) describes event CATEGORIES the generic org-wide
// calendar mock happens to seed; this vaccination-only slice colors rows by OWNER (PC / Inventory /
// Admin-Data-Ops) instead, since that is the real dimension `ownerColor` renders — same `.legend`/`.sw`
// anatomy, honest content for the data actually on screen.
export function OwnerLegend({ ownerMeta }: { ownerMeta: OwnerPresentationMap }) {
  const entries = Object.entries(ownerMeta).filter(([key]) => key !== "all");
  if (!entries.length) return null;
  return (
    <span className="legend">
      {entries.map(([key, meta]) => (
        <span key={key}>
          <span className="sw" style={{ background: meta.color }} />
          {meta.label}
        </span>
      ))}
    </span>
  );
}

export function RemindersRail({
  reminderRail,
  presentation,
  eventHref,
}: {
  reminderRail: CalendarReminderRail | null | undefined;
  presentation: CalendarPresentation;
  eventHref: (id: string) => string;
}) {
  const items = reminderRail?.items ?? [];
  const totalCount = reminderRail?.count ?? 0;
  const omittedCount = totalCount - items.length;
  return (
    <div className="card">
      <div className="hd">
        <Clock className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />
        <h3>{presentation.week.reminder_title}</h3>
      </div>
      <div className="bd feed">
        {items.length === 0 ? (
          <p className="muted small" style={{ margin: "4px 2px" }}>
            {reminderRail?.empty_message ?? presentation.week.reminder_empty_message}
          </p>
        ) : (
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          items.map((item: any) => {
            const badgeLabel = item.escalation_label || item.reminder_label || "";
            return (
              <Link key={item.event_id} href={eventHref(item.event_id)} replace scroll={false} className="fitem">
                <span className="fic" style={{ background: `color-mix(in srgb, var(--brand) 20%, var(--panel))`, color: "var(--brand)" }}>
                  <Bell className="ic" aria-hidden="true" />
                </span>
                <div className="tx">
                  <b>{item.title}</b>
                  <div className="mt">{item.subtitle}</div>
                  {item.channels && item.channels.length ? (
                    <div style={{ marginTop: 4, display: "flex", gap: 4, flexWrap: "wrap" }}>
                      {item.channels.map((channel: string) => (
                        <Tag key={channel} tone="info">
                          {channel}
                        </Tag>
                      ))}
                    </div>
                  ) : null}
                </div>
                {badgeLabel ? <span className="erem">{badgeLabel}</span> : null}
              </Link>
            );
          })
        )}
      </div>
      {items.length ? (
        <div className="note" style={{ marginTop: 10 }}>
          <span className="muted small">
            {totalCount > 0 ? `Showing ${items.length} of ${totalCount}` : presentation.week.reminder_note}
          </span>
          {omittedCount > 0 ? (
            <span className="muted small" style={{ display: "block", marginTop: 4 }}>
              +{omittedCount} more not shown
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function RhythmCard({
  rhythmDayHref,
  todayWeekday,
  dayFilter,
  presentation,
}: {
  rhythmDayHref: (day: CalendarRhythmDay) => string;
  todayWeekday: string;
  dayFilter?: string;
  presentation: CalendarPresentation;
}) {
  const days = presentation.rhythm.days;
  if (!days.length) return null;
  return (
    <div className="card" style={{ marginBottom: 16 }}>
      <div className="bd">
        <div className="b700" style={{ marginBottom: 9 }}>
          {presentation.rhythm.title} <span className="muted small">— {presentation.rhythm.note}</span>
        </div>
        <div className="rhythm">
          {days.map((day) => (
            <Link
              key={day.day}
              href={rhythmDayHref(day)}
              replace
              scroll={false}
              className={`rday${day.day === dayFilter ? " included" : ""}${day.day === todayWeekday ? " today" : ""}`}
              aria-disabled={day.enabled ? undefined : "true"}
            >
              <div className="d">{day.day}</div>
              <div className={`mode ${day.tone}`}>{day.label}</div>
            </Link>
          ))}
        </div>
      </div>
    </div>
  );
}
