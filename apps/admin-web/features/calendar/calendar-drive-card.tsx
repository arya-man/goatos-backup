// Park-level vaccination drive progress card — replaces the old flat 4-tile
// SHEDS/VACCINES/DOSES/DRIVE PACKETS metagrid for aggregated `event.aggregated` drive rows. A drive
// clubs multiple sheds + vaccines under one park/day, so this renders park-level rollup progress
// instead of raw tile counts. Source of truth is `event.drive_summary` (see the shim + field-shape
// contract in calendar-contract.ts). Never compute these counts client-side from paginated targets.
import { Syringe } from "lucide-react";
import { copy, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate as fmtIstDate } from "@/lib/format";
import { driveSummaryOf, type CalendarEvent } from "./calendar-contract";
import { driveCoverage, driveCoveragePct, driveStatusChips } from "./drive-card-metrics";

export function DriveProgressCard({ event, pageContract }: { event: CalendarEvent; pageContract: AdminUiPageContract }) {
  const summary = driveSummaryOf(event);
  if (!summary) {
    // drive_summary not present yet on this row (backend field regen in flight, or a genuinely
    // uncovered drive shape) — stay honest with a simplified card at the fields we DO have
    // (top-level shed_count/vaccine_count are already real, non-drive-summary CalendarEvent fields).
    return (
      <div className="ev" aria-disabled="true" title={copy(pageContract, "calendar.drive.summary_pending")}>
        <div className="dhd">
          <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <b>{event.title}</b>
        </div>
        <div className="prow">
          <div className="muted" style={{ fontSize: "12px" }}>{copy(pageContract, "calendar.drive.summary_pending")}</div>
        </div>
      </div>
    );
  }

  // Coverage ring + the "N/N" headline prefer the DISTINCT-ANIMAL grain (total_animals /
  // completed_animals): a goat due for several vaccines the same day is ONE animal, and is only
  // "completed" once all its drive obligations are done. If a mixed-version response omits those
  // fields (CDR-R1) they fall back to the obligation/dose counts, labelled accordingly. The
  // status chips below always stay obligation-grain (dose work items).
  const coverage = driveCoverage(summary.completed_animals, summary.total_animals, summary.completed_count, summary.total_count);
  const pct = driveCoveragePct(coverage.completed, coverage.total);
  const chips = driveStatusChips(summary);

  // Completion ring. Geometry: r=29 on a 70x70 viewBox, stroke-width 7, round linecap,
  // rotated -90deg so the arc starts at 12 o'clock. circumference = 2*pi*r ≈ 182.2.
  const ringRadius = 29;
  const ringCircumference = 2 * Math.PI * ringRadius;
  const ringOffset = ringCircumference * (1 - pct / 100);

  return (
    <div className="ev">
      <div className="dhd">
        <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <b>
          {event.title} · {summary.park_name}
        </b>
        <span style={{ color: "var(--muted)", fontSize: "12px" }}>{fmtIstDate(summary.due_date)}</span>
      </div>
      <div className="prow">
        <svg className="ring" viewBox="0 0 70 70" width="64" height="64" aria-hidden="true">
          <circle className="rbg" cx="35" cy="35" r={ringRadius} />
          <circle
            className="rfg"
            cx="35"
            cy="35"
            r={ringRadius}
            strokeDasharray={ringCircumference.toFixed(1)}
            strokeDashoffset={ringOffset.toFixed(1)}
            transform="rotate(-90 35 35)"
          />
          <text x="35" y="35" className="rtx" textAnchor="middle" dominantBaseline="central">
            {pct}%
          </text>
        </svg>
        <div className="ptx">
          <div>
            <span className="big">{coverage.completed}</span>
            <span className="u"> / {coverage.total} {coverage.usesAnimals ? "animals" : "doses"}</span>
          </div>
          <div className="metric">
            <b>{summary.sheds_completed}</b> of {summary.shed_count} sheds done
          </div>
          <span className="pill">{summary.vaccine_labels.length} vaccines</span>
        </div>
      </div>
      {chips.length > 0 ? (
        <div className="chips">
          {chips.map((chip) => (
            <div key={chip.key} className={`sc ${chip.key}`}>
              <div className={`d c-${chip.key}`} />
              {chip.count} {optionLabel(pageContract, "calendar_status", chip.key).toLowerCase()}
            </div>
          ))}
        </div>
      ) : null}
      <div className="dft">Owner · {summary.owner_label}</div>
    </div>
  );
}
