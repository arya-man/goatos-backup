// Park-level vaccination drive progress card — replaces the old flat 4-tile
// SHEDS/VACCINES/DOSES/DRIVE PACKETS metagrid for aggregated `event.aggregated` drive rows. A drive
// clubs multiple sheds + vaccines under one park/day, so this renders park-level rollup progress
// instead of raw tile counts. Source of truth is `event.drive_summary` (see the shim + field-shape
// contract in calendar-contract.ts). Never compute these counts client-side from paginated targets.
import { Syringe } from "lucide-react";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate as fmtIstDate } from "@/lib/format";
import { driveSummaryOf, type CalendarDriveSummary, type CalendarEvent } from "./calendar-contract";

type RemainingKey = "due" | "overdue" | "deferred";

function headlineStatus(summary: CalendarDriveSummary, pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  if (summary.overdue_count > 0) {
    return { label: `${summary.overdue_count} ${optionLabel(pageContract, "calendar_status", "overdue").toLowerCase()}`, tone: optionTone(pageContract, "calendar_status", "overdue") as Tone };
  }
  if (summary.total_count > 0 && summary.remaining_count <= 0) {
    return { label: optionLabel(pageContract, "calendar_status", "completed"), tone: optionTone(pageContract, "calendar_status", "completed") as Tone };
  }
  if (summary.deferred_count > 0 && summary.due_count === 0) {
    return { label: optionLabel(pageContract, "calendar_status", "deferred").toLowerCase(), tone: optionTone(pageContract, "calendar_status", "deferred") as Tone };
  }
  return { label: `${summary.due_count} ${optionLabel(pageContract, "calendar_status", "due").toLowerCase()}`, tone: optionTone(pageContract, "calendar_status", "due") as Tone };
}

function remainingChips(summary: CalendarDriveSummary): { key: RemainingKey; count: number }[] {
  return (
    [
      { key: "due", count: summary.due_count },
      { key: "overdue", count: summary.overdue_count },
      { key: "deferred", count: summary.deferred_count },
    ] as const
  ).filter((chip) => chip.count > 0);
}

export function DriveProgressCard({ event, pageContract }: { event: CalendarEvent; pageContract: AdminUiPageContract }) {
  const summary = driveSummaryOf(event);
  if (!summary) {
    // drive_summary not present yet on this row (backend field regen in flight, or a genuinely
    // uncovered drive shape) — stay honest with the mock-shaped card at the fields we DO have
    // (top-level shed_count/vaccine_count are already real, non-drive-summary CalendarEvent fields)
    // instead of either hiding the row or fabricating progress numbers.
    return (
      <div className="metagrid" style={{ marginTop: 10 }} aria-disabled="true" title={copy(pageContract, "calendar.drive.summary_pending")}>
        <div>
          <div className="k">{copy(pageContract, "calendar.drive.sheds")}</div>
          <div className="v">{event.shed_count}</div>
        </div>
        <div>
          <div className="k">{copy(pageContract, "calendar.drive.vaccines")}</div>
          <div className="v">{event.vaccine_count}</div>
        </div>
        <div style={{ gridColumn: "1/3" }}>
          <div className="k">{copy(pageContract, "calendar.drive.packets")}</div>
          <div className="v muted">{copy(pageContract, "calendar.drive.summary_pending")}</div>
        </div>
      </div>
    );
  }

  const pct = summary.total_count > 0 ? Math.round((summary.completed_count / summary.total_count) * 100) : 0;
  const headline = headlineStatus(summary, pageContract);
  const remaining = remainingChips(summary);
  // Option A · completion ring. Geometry: r=29 on a 70x70 viewBox, stroke-width 7, round linecap,
  // rotated -90deg so the arc starts at 12 o'clock. circumference = 2*pi*r ≈ 182.2.
  const ringRadius = 29;
  const ringCircumference = 2 * Math.PI * ringRadius;
  const ringOffset = ringCircumference * (1 - pct / 100);

  return (
    <div className="drivecard" style={{ marginTop: 10 }}>
      <div className="drivecard-hd">
        <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <b>
          {event.title} · {summary.park_name}
        </b>
        <span className="muted small">{fmtIstDate(summary.due_date)}</span>
        <span className="sp" />
        <Tag tone={headline.tone}>{headline.label}</Tag>
      </div>
      <div className="muted small" style={{ marginTop: 6 }}>
        {summary.vaccine_labels.length} {copy(pageContract, "calendar.drive.vaccines_suffix")}
      </div>
      <div className="drivecard-progress" style={{ marginTop: 10 }}>
        <svg className="ring" viewBox="0 0 70 70" width="68" height="68" aria-hidden="true">
          <circle className="ring-track" cx="35" cy="35" r={ringRadius} />
          <circle
            className="ring-arc"
            cx="35"
            cy="35"
            r={ringRadius}
            strokeDasharray={ringCircumference.toFixed(1)}
            strokeDashoffset={ringOffset.toFixed(1)}
            transform="rotate(-90 35 35)"
          />
          <text x="35" y="35" className="ring-pct" textAnchor="middle" dominantBaseline="central">
            {pct}%
          </text>
        </svg>
        <div className="drivecard-progress-txt">
          <div>
            <b>{summary.completed_count}</b>{" "}
            <span className="muted">
              / {summary.total_count} {copy(pageContract, "calendar.drive.doses")}
            </span>
          </div>
          <div className="muted small">
            {summary.sheds_completed} {copy(pageContract, "calendar.drive.of")} {summary.shed_count}{" "}
            {copy(pageContract, "calendar.drive.sheds_done_suffix")}
          </div>
        </div>
      </div>
      {remaining.length ? (
        <div className="drivecard-chips" style={{ marginTop: 8 }}>
          {remaining.map((chip) => (
            <Tag key={chip.key} tone={optionTone(pageContract, "calendar_status", chip.key) as Tone}>
              {chip.count} {optionLabel(pageContract, "calendar_status", chip.key).toLowerCase()}
            </Tag>
          ))}
        </div>
      ) : null}
      <div className="drivecard-ft">
        <span className="muted small">{summary.owner_label}</span>
        <span className="lenslink mutedlink">{copy(pageContract, "calendar.drive.open")} →</span>
      </div>
    </div>
  );
}
