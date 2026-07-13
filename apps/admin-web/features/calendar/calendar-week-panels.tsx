// Week-view side panels ported from the ops-console calendar mock's anatomy: the owner-color legend
// and the weekly operating-rhythm strip (`.rhythm`/`.rday`). Both read real backend-compiled contract
// data (`CalendarPresentation.rhythm`), already served by `/admin-web/bootstrap` — see
// backend/internal/adminui/app/service.go — rather than inventing local copy.
import Link from "next/link";
import {
  type CalendarPresentation,
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
