import Link from "@/components/no-prefetch-link";
import { CalendarDays, Layers, MapPinned, Warehouse } from "lucide-react";
import {
  getVaccinationDriveAssignments,
  type ApiResult,
  type VaccinationDriveAssignmentResponse,
} from "@/lib/api/server";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate, todayIso } from "@/lib/format";
import { backendScope, scopeHref, type Scope } from "@/lib/scope";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { ClipText, Tag } from "@/components/ui-primitives";

const CURRENT_YEAR = Number(todayIso().slice(0, 4));
const CURRENT_MONTH = Number(todayIso().slice(5, 7));
const PAGE_LIMIT = 2000;

function selectedScheduleYear(searchParams: RouteSearchParams | undefined): number {
  return boundedInt(one(searchParams ?? {}, "schedule_year"), CURRENT_YEAR, CURRENT_YEAR - 1, CURRENT_YEAR + 5);
}

function selectedScheduleMonth(searchParams: RouteSearchParams | undefined): number {
  return boundedInt(one(searchParams ?? {}, "schedule_month"), CURRENT_MONTH, 1, 12);
}

function monthLabel(year: number, month: number): string {
  return new Intl.DateTimeFormat("en", { month: "short", year: "numeric", timeZone: "Asia/Kolkata" }).format(new Date(Date.UTC(year, month - 1, 1)));
}

function dateEyebrow(date: string): string {
  if (!date) return "";
  return new Intl.DateTimeFormat("en", { weekday: "short", timeZone: "Asia/Kolkata" }).format(new Date(`${date}T00:00:00+05:30`));
}

function partitionLabel(pageContract: AdminUiPageContract, label: string): string {
  const trimmed = label.trim();
  if (!trimmed) return copy(pageContract, "schedule.partition.whole_shed");
  return /^part\b/i.test(trimmed) ? trimmed : `${copy(pageContract, "schedule.partition.prefix")} ${trimmed}`;
}

async function loadDriveSchedule(scope: Scope, year: number, month: number): Promise<ApiResult<VaccinationDriveAssignmentResponse>> {
  const { parkId } = backendScope(scope);
  return getVaccinationDriveAssignments({ parkId, year, month, limit: PAGE_LIMIT });
}

export async function loadVaccinationFullSchedule(searchParams: RouteSearchParams | undefined, scope: Scope) {
  return loadDriveSchedule(scope, selectedScheduleYear(searchParams), selectedScheduleMonth(searchParams));
}

export function vaccinationScheduleYear(searchParams: RouteSearchParams | undefined): number {
  return selectedScheduleYear(searchParams);
}

export function VaccinationFullScheduleSkeleton({
  pageContract,
}: {
  pageContract: AdminUiPageContract;
}) {
  return (
    <section id="full-schedule" className="card vaccination-schedule-card" style={{ scrollMarginTop: 80 }} aria-busy="true">
      <div className="hd vaccination-schedule-hd">
        <CalendarDays className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <div style={{ minWidth: 0 }}>
          <h3>{copy(pageContract, "section.full_schedule.operator_title")}</h3>
          <span className="muted small">{copy(pageContract, "section.full_schedule.loading_operator_note")}</span>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {[70, 70, 120].map((w, i) => (
          <div key={i} className="skel" style={{ width: w, height: 30, borderRadius: 999 }} />
        ))}
      </div>
      <div className="bd" style={{ display: "grid", gap: 12 }}>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skel" style={{ width: 62, height: 28, borderRadius: 999 }} />
          ))}
        </div>
        <div style={{ overflowX: "auto" }}>
          <table className="shed-summary-table">
            <thead>
              <tr>
                <th>{copy(pageContract, "schedule.column.date")}</th>
                <th>{copy(pageContract, "schedule.column.operator")}</th>
                <th>{copy(pageContract, "schedule.column.park")}</th>
                <th>{copy(pageContract, "schedule.column.shed")}</th>
                <th>{copy(pageContract, "schedule.column.partition")}</th>
                <th className="num">{copy(pageContract, "schedule.column.animals")}</th>
                <th>{copy(pageContract, "schedule.column.capacity")}</th>
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 5 }, (_, row) => (
                <tr key={row}>
                  {Array.from({ length: 7 }, (_, col) => (
                    <td key={col}>
                      <span className="skel" style={{ width: col === 5 ? 48 : 96, height: 16 }} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
}

export async function VaccinationFullSchedule({
  searchParams,
  scope,
  pageContract,
  scheduleResult,
}: {
  searchParams?: RouteSearchParams;
  scope: Scope;
  pageContract: AdminUiPageContract;
  scheduleResult?: ApiResult<VaccinationDriveAssignmentResponse>;
}) {
  const year = selectedScheduleYear(searchParams);
  const month = selectedScheduleMonth(searchParams);
  const result = scheduleResult ?? (await loadDriveSchedule(scope, year, month));
  const rows = result.ok ? result.data.rows : [];
  const parks = new Set(rows.map((row) => row.parkId).filter(Boolean));
  const sheds = new Set(rows.map((row) => `${row.parkId}|${row.physicalShed}`).filter(Boolean));
  const animals = rows.reduce((sum, row) => sum + row.animals, 0);

  function yearHref(nextYear: number) {
    return scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(nextYear), schedule_month: String(month) });
  }

  function monthHref(nextMonth: number) {
    return scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year), schedule_month: String(nextMonth) });
  }

  return (
    <section id="full-schedule" className="card vaccination-schedule-card" style={{ scrollMarginTop: 80 }}>
      <div className="hd vaccination-schedule-hd">
        <CalendarDays className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <div style={{ minWidth: 0 }}>
          <h3>{copy(pageContract, "section.full_schedule.operator_title")}</h3>
          <span className="muted small">{copy(pageContract, "section.full_schedule.operator_note")}</span>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href={yearHref(year - 1)} className="chip" scroll={false} prefetch={false}>
          {year - 1}
        </Link>
        <Link href={yearHref(year)} className="chip on" scroll={false} prefetch={false} aria-current="page">
          {year}
        </Link>
        <Link href={yearHref(year + 1)} className="chip" scroll={false} prefetch={false}>
          {copy(pageContract, "action.next_year")} {year + 1}
        </Link>
        <Link href={scopeHref("/vaccination", scope)} className="btn sm" scroll={false} prefetch={false}>
          {copy(pageContract, "action.open_shed_board")}
        </Link>
      </div>

      <div className="vaccination-schedule-summary compact">
        <div className="kpi">
          <MapPinned className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.parks")}</span>
          <b>{parks.size}</b>
        </div>
        <div className="kpi">
          <Warehouse className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.sheds")}</span>
          <b>{sheds.size}</b>
        </div>
        <div className="kpi">
          <Layers className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.animals_assigned")}</span>
          <b>{animals}</b>
        </div>
        <div className="kpi">
          <CalendarDays className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.drive_rows")}</span>
          <b>{rows.length}</b>
        </div>
      </div>

      <div className="chips vaccination-schedule-legend" aria-label={copy(pageContract, "schedule.legend.aria")}>
        {Array.from({ length: 12 }, (_, idx) => idx + 1).map((m) => (
          <Link key={m} href={monthHref(m)} className={m === month ? "chip on" : "chip"} scroll={false} prefetch={false} aria-current={m === month ? "page" : undefined}>
            {monthLabel(year, m)}
          </Link>
        ))}
      </div>

      {!result.ok ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "18px 16px", flexWrap: "wrap" }}>
          <Layers className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--danger)", flexShrink: 0 }} />
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>{copy(pageContract, "section.full_schedule.assignment_unavailable_title")}</b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {result.error.message}
            </span>
          </div>
        </div>
      ) : rows.length === 0 ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "18px 16px", flexWrap: "wrap" }}>
          <Layers className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} />
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>{copy(pageContract, "section.full_schedule.no_assignments_title")}</b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {copy(pageContract, "section.full_schedule.no_assignments_body")} {monthLabel(year, month)}.
            </span>
          </div>
        </div>
      ) : (
        <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.full_schedule.operator_title")}>
          <table className="full-vaccine-schedule-table">
            <thead>
              <tr>
                <th>{copy(pageContract, "schedule.column.date")}</th>
                <th>{copy(pageContract, "schedule.column.operator")}</th>
                <th>{copy(pageContract, "schedule.column.park")}</th>
                <th>{copy(pageContract, "schedule.column.shed")}</th>
                <th>{copy(pageContract, "schedule.column.partition")}</th>
                <th className="num">{copy(pageContract, "schedule.column.animals")}</th>
                <th>{copy(pageContract, "schedule.column.capacity")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={`${row.plannedDate}|${row.operatorId}|${row.physicalShed}|${row.partitionLabel}`} className="schedule-click-row">
                  <td className="schedule-date-cell state-scheduled">
                    <span className="schedule-date-stack">
                      <span className="schedule-date-main">{fmtDate(row.plannedDate)}</span>
                      <span className="schedule-date-sub">{dateEyebrow(row.plannedDate)}</span>
                    </span>
                  </td>
                  <td><b>{row.operatorName}</b></td>
                  <td><ClipText title={row.parkName}>{row.parkName}</ClipText></td>
                  <td><ClipText title={row.physicalShed}>{row.physicalShed}</ClipText></td>
                  <td>{row.partitionLabel ? <Tag tone="info">{partitionLabel(pageContract, row.partitionLabel)}</Tag> : <span className="muted">{copy(pageContract, "schedule.partition.whole_shed")}</span>}</td>
                  <td className="num"><b>{row.animals}</b></td>
                  <td><Tag tone={row.capacity === "capacity_breach" ? "dng" : "ok"}>{row.capacity}</Tag></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
