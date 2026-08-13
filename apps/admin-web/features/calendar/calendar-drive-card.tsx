// Park-level vaccination drive progress card — replaces the old flat 4-tile
// SHEDS/VACCINES/DOSES/DRIVE PACKETS metagrid for aggregated `event.aggregated` drive rows. A drive
// clubs multiple sheds + vaccines under one park/day, so this renders park-level rollup progress
// instead of raw tile counts. Source of truth is `event.drive_summary` (see the shim + field-shape
// contract in calendar-contract.ts). Never compute these counts client-side from paginated targets.
import { Syringe } from "lucide-react";
import { copy, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate as fmtIstDate } from "@/lib/format";
import { driveSummaryOf, type CalendarEvent } from "./calendar-contract";
import { driveVisibleProgress, drivePctFor, driveStatusChips, driveStatusClass } from "./drive-card-metrics";

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

  // Coverage ring + the "N/N" headline render the BACKEND-OWNED progress numerator, denominator and
  // grain verbatim (progress_completed / progress_total / progress_basis / progress_pct). The same
  // contract drives the Android card, so both surfaces always show the same number and the same ring
  // percentage. Do NOT re-derive a numerator here. The status chips below stay obligation-grain
  // and are explicitly labelled as doses so a multi-vaccine drive cannot look like duplicate animals.
  const coverage = driveVisibleProgress(summary);
  const submittedAnimals = "submitted_animals" in summary && typeof summary.submitted_animals === "number" ? summary.submitted_animals : 0;
  const hasSubmittedPending = submittedAnimals > coverage.completed;
  const pct = drivePctFor(summary, coverage);
  const chips = driveStatusChips(summary);
  const vaccineLabels = summary.vaccine_labels.filter((label) => label.trim().length > 0);
  const visibleVaccines = vaccineLabels.slice(0, 3);
  const hiddenVaccineCount = Math.max(0, vaccineLabels.length - visibleVaccines.length);

  // Completion ring. Geometry: r=29 on a 70x70 viewBox, stroke-width 7, round linecap,
  // rotated -90deg so the arc starts at 12 o'clock. circumference = 2*pi*r ≈ 182.2.
  const ringRadius = 29;
  const ringCircumference = 2 * Math.PI * ringRadius;
  const ringOffset = ringCircumference * (1 - pct / 100);

  return (
    <div className="ev drive-card">
      <div className="dhd">
        <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <b>
          {event.title} · {summary.park_name}
        </b>
        <span style={{ color: "var(--muted)", fontSize: "12px" }}>{fmtIstDate(summary.due_date)}</span>
      </div>
      <div className="prow">
        <svg className="dring" viewBox="0 0 70 70" width="64" height="64" aria-hidden="true">
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
            <span className="u"> / {coverage.total} {copy(pageContract, coverage.usesAnimals ? "calendar.drive.animals" : "calendar.drive.doses")}</span>
          </div>
          {hasSubmittedPending ? (
            <div className="submitted-strip" title={`${submittedAnimals} ${copy(pageContract, "calendar.drive.verification_pending")}`}>
              <span>{copy(pageContract, "calendar.drive.verification_pending")}</span>
            </div>
          ) : null}
          <div className="metric">
            <b>{summary.sheds_completed}</b> {copy(pageContract, "calendar.drive.of")} {summary.shed_count} {copy(pageContract, "calendar.drive.sheds_done_suffix")}
          </div>
        </div>
      </div>
      {visibleVaccines.length > 0 ? (
        <div className="vaccine-chip-row" aria-label={vaccineLabels.join(", ")}>
          {visibleVaccines.map((label) => (
            <span key={label} className="vaccine-chip" title={label}>{label}</span>
          ))}
          {hiddenVaccineCount > 0 ? (
            <span className="vaccine-chip vaccine-chip-more" title={vaccineLabels.join(", ")}>+{hiddenVaccineCount}</span>
          ) : null}
        </div>
      ) : null}
      <div className="dft">
        {copy(pageContract, "calendar.drive.name")} · {summary.drive_name}
      </div>
      <div className="dft">
        {copy(pageContract, "calendar.drive.total")} · {summary.drive_total} {copy(pageContract, "calendar.drive.animals")}
      </div>
      {chips.length > 0 ? (
        <div className="chips">
          {chips.map((chip) => (
            <div key={chip.key} className={`sc ${driveStatusClass(chip.key)}`}>
              <div className={`d c-${driveStatusClass(chip.key)}`} />
              {chip.count} {copy(pageContract, "calendar.drive.doses").toLowerCase()} {optionLabel(pageContract, "calendar_status", chip.key).toLowerCase()}
            </div>
          ))}
        </div>
      ) : null}
      <div className="dft">{copy(pageContract, "calendar.drive.owner")} · {summary.owner_label}</div>
    </div>
  );
}
