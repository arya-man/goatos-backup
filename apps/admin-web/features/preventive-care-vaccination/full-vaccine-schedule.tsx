import Link from "@/components/no-prefetch-link";
import type { ReactNode } from "react";
import { CalendarDays, Layers, MapPinned, Warehouse } from "lucide-react";
import {
  getVaccinationSchedule,
  type ApiResult,
  type VaccinationOperationsCell,
  type VaccinationOperationsCohort,
  type VaccinationOperationsProtocol,
  type VaccinationOperationsResponse,
} from "@/lib/api/server";
import { copy, optionGroup, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate, todayIso } from "@/lib/format";
import { backendScope, scopeHref, type Scope } from "@/lib/scope";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { ClipText, Tag, type Tone } from "@/components/ui-primitives";
import { sortVaccinationProtocols, vaccinationDriveDisplayName, vaccinationVaccineDisplayName } from "./vaccine-display";
import { vaccinationScheduleCellAsOf, vaccinationScheduleCohortAsOf } from "./full-vaccine-schedule-links";
import { isInScheduleMonth } from "./full-vaccine-schedule-month";

const CURRENT_YEAR = Number(todayIso().slice(0, 4));
const CURRENT_MONTH = Number(todayIso().slice(5, 7));
const PAGE_LIMIT = 500;

type ScheduleState = "overdue" | "due_soon" | "up_to_date" | "scheduled" | "no_record";

const STATE_TONE: Record<ScheduleState, Tone> = {
  overdue: "dng",
  due_soon: "warn",
  up_to_date: "ok",
  scheduled: "info",
  no_record: "mut",
};

type ScheduleCellView = {
  state: ScheduleState;
  label: string;
  title: string;
};

function selectedScheduleYear(searchParams: RouteSearchParams | undefined): number {
  return boundedInt(one(searchParams ?? {}, "schedule_year"), CURRENT_YEAR, CURRENT_YEAR - 1, CURRENT_YEAR + 5);
}

function selectedScheduleMonth(searchParams: RouteSearchParams | undefined): number {
  return boundedInt(one(searchParams ?? {}, "schedule_month"), CURRENT_MONTH, 1, 12);
}

function selectedScheduleCursor(searchParams: RouteSearchParams | undefined): string | undefined {
  const cursor = one(searchParams ?? {}, "schedule_cursor");
  return cursor && cursor.length <= 512 ? cursor : undefined;
}

function monthLabel(year: number, month: number): string {
  return new Intl.DateTimeFormat("en", { month: "short", year: "numeric", timeZone: "Asia/Kolkata" }).format(new Date(Date.UTC(year, month - 1, 1)));
}

function humanize(value: string | undefined): string {
  if (!value) return "";
  return value
    .replace(/[_-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b\w/g, (m) => m.toUpperCase());
}

function rowType(cohort: VaccinationOperationsCohort, pageContract: AdminUiPageContract): string {
  const stage = humanize(cohort.stage);
  const ageBand = (cohort.ageBand ?? "").toLowerCase();
  if (ageBand === "adult") {
    if (stage && stage.toLowerCase() !== "adult") return `${stage} source · adult course`;
    return copy(pageContract, "schedule.row_type.adult");
  }
  if (ageBand === "kid") {
    if (stage && stage.toLowerCase() !== "kid") return `${stage} source · kid course`;
    return copy(pageContract, "schedule.row_type.kid");
  }
  return stage || copy(pageContract, "schedule.row_type.fallback");
}

function isScheduleWrapperProtocol(protocol: VaccinationOperationsProtocol): boolean {
  return /preventive care vaccination matrix/i.test(protocol.name);
}

function scheduleCellView(
  cell: VaccinationOperationsCell | undefined,
  year: number,
  month: number,
  pageContract: AdminUiPageContract,
): ScheduleCellView {
  if (!cell) {
    return {
      state: "no_record",
      label: copy(pageContract, "schedule.cell.no_record"),
      title: copy(pageContract, "schedule.cell.no_record_title"),
    };
  }
  const dateIso = isInScheduleMonth(cell.nextDue, year, month) ? cell.nextDue : isInScheduleMonth(cell.lastDose, year, month) ? cell.lastDose : undefined;
  if (!dateIso) {
    return {
      state: "no_record",
      label: copy(pageContract, "schedule.cell.no_record"),
      title: copy(pageContract, "schedule.cell.outside_year_title"),
    };
  }
  const label = fmtDate(dateIso);
  if (cell.workState === "overdue" || cell.workState === "missed" || cell.workState === "rejected") {
    return { state: "overdue", label, title: copy(pageContract, "schedule.cell.overdue_title") };
  }
  if (cell.workState === "scheduled" || cell.workState === "in_progress") {
    return { state: "scheduled", label, title: copy(pageContract, "schedule.cell.scheduled_title") };
  }
  if (cell.workState === "due" || cell.workState === "proof_pending" || cell.workState === "verification_pending") {
    return { state: "due_soon", label, title: copy(pageContract, "schedule.cell.due_title") };
  }
  if (cell.workState === "completed" || cell.counts.accepted > 0) {
    return { state: "up_to_date", label, title: copy(pageContract, "schedule.cell.up_to_date_title") };
  }
  return { state: "no_record", label, title: copy(pageContract, "schedule.cell.no_record_title") };
}

function scheduleCellVaccines(cell: VaccinationOperationsCell | undefined, pageContract: AdminUiPageContract): string {
  const labels = Array.from(new Set((cell?.vaccineNames ?? []).map(vaccinationVaccineDisplayName).filter(Boolean)));
  return labels.length > 0 ? labels.join(", ") : copy(pageContract, "schedule.cell.no_record");
}

async function loadFullSchedule(scope: Scope, year: number, month: number, cursor?: string): Promise<ApiResult<VaccinationOperationsResponse>> {
  const { parkId } = backendScope(scope);
  return getVaccinationSchedule({
    parkId,
    year,
    month,
    cursor,
    limit: PAGE_LIMIT,
  });
}

export async function loadVaccinationFullSchedule(searchParams: RouteSearchParams | undefined, scope: Scope) {
  return loadFullSchedule(scope, selectedScheduleYear(searchParams), selectedScheduleMonth(searchParams), selectedScheduleCursor(searchParams));
}

export function vaccinationScheduleYear(searchParams: RouteSearchParams | undefined): number {
  return selectedScheduleYear(searchParams);
}

export function VaccinationFullScheduleSkeleton({
  pageContract,
}: {
  pageContract: AdminUiPageContract;
}) {
  const scheduleTable = table(pageContract, "full-vaccine-schedule");
  const fixedColumns = scheduleTable.columns.filter((column) => column.visible);
  return (
    <section id="full-schedule" className="card vaccination-schedule-card" style={{ scrollMarginTop: 80 }} aria-busy="true">
      <div className="hd vaccination-schedule-hd">
        <CalendarDays className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <div style={{ minWidth: 0 }}>
          <h3>{copy(pageContract, "section.full_schedule.title")}</h3>
          <span className="muted small">{copy(pageContract, "section.full_schedule.note")}</span>
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
                {fixedColumns.map((col) => (
                  <th key={col.key}>{col.label}</th>
                ))}
                {Array.from({ length: 5 }, (_, i) => (
                  <th key={i}><span className="skel" style={{ width: 96, height: 14 }} /></th>
                ))}
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 5 }, (_, row) => (
                <tr key={row}>
                  {Array.from({ length: fixedColumns.length + 5 }, (_, col) => (
                    <td key={col}>
                      <span className="skel" style={{ width: col < fixedColumns.length ? 110 : 72, height: 16 }} />
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
  scheduleResult?: ApiResult<VaccinationOperationsResponse>;
}) {
  const year = selectedScheduleYear(searchParams);
  const month = selectedScheduleMonth(searchParams);
  const cursor = selectedScheduleCursor(searchParams);
  const result = scheduleResult ?? (await loadFullSchedule(scope, year, month, cursor));
  const protocols = result.ok ? sortVaccinationProtocols(result.data.protocols) : [];
  const rows = result.ok
    ? [...result.data.cohorts].sort((a, b) =>
        [a.parkName, a.shedName, rowType(a, pageContract)]
          .join("|")
          .localeCompare([b.parkName, b.shedName, rowType(b, pageContract)].join("|")),
      )
    : [];
  const uniqueParks = new Set(rows.map((row) => row.parkId));
  const uniqueSheds = new Set(rows.map((row) => row.shedId));
  let activeCells = 0;
  let overdueCells = 0;
  for (const row of rows) {
    for (const protocol of protocols) {
      const state = scheduleCellView(row.cells.find((cell) => cell.protocolId === protocol.protocolId), year, month, pageContract).state;
      if (state !== "no_record") activeCells += 1;
      if (state === "overdue") overdueCells += 1;
    }
  }
  const totalAnimals = rows.reduce((sum, row) => sum + row.animals, 0);
  const scheduleTable = table(pageContract, "full-vaccine-schedule");
  const fixedColumns = scheduleTable.columns.filter((column) => column.visible);
  const legend = optionGroup(pageContract, "schedule_status_legend");
  const singleScheduleColumn = protocols.length === 1 && protocols.every(isScheduleWrapperProtocol);
  const currentScheduleHref = scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year), schedule_month: String(month) });
  const freshness = result.ok ? result.data.freshness : undefined;
  const staleSchedule = Boolean(freshness?.stale || freshness?.rebuildRequired || (freshness?.status && freshness.status !== "green"));
  const nextScheduleHref =
    result.ok && result.data.next_cursor
      ? scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year), schedule_month: String(month), schedule_cursor: result.data.next_cursor })
      : null;

  function yearHref(nextYear: number) {
    return scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(nextYear), schedule_month: String(month) });
  }

  function monthHref(nextMonth: number) {
    return scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year), schedule_month: String(nextMonth) });
  }

  function detailHref(row: VaccinationOperationsCohort, asOf?: string): string {
    return scopeHref(
      `/vaccination/execution/sheds/${encodeURIComponent(row.shedId)}`,
      scope,
      { mode: "park", park: row.parkId, asOf: asOf ?? null },
      { ret: currentScheduleHref },
    );
  }

  function rowLink(row: VaccinationOperationsCohort, content: ReactNode, className?: string) {
    return (
      <Link
        href={detailHref(row, vaccinationScheduleCohortAsOf(row, year))}
        className={className ? `celllink ${className}` : "celllink"}
        scroll={false}
        prefetch={false}
        title={copy(pageContract, "schedule.row.open_title")}
      >
        {content}
      </Link>
    );
  }

  return (
    <section id="full-schedule" className="card vaccination-schedule-card" style={{ scrollMarginTop: 80 }}>
      <div className="hd vaccination-schedule-hd">
        <CalendarDays className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <div style={{ minWidth: 0 }}>
          <h3>{copy(pageContract, "section.full_schedule.title")}</h3>
          <span className="muted small">{copy(pageContract, "section.full_schedule.note")}</span>
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
          <b>{uniqueParks.size}</b>
        </div>
        <div className="kpi">
          <Warehouse className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.sheds")}</span>
          <b>{uniqueSheds.size}</b>
        </div>
        <div className="kpi">
          <Layers className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.animals")}</span>
          <b>{totalAnimals}</b>
        </div>
        <div className="kpi">
          <CalendarDays className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.cells")}</span>
          <b>{activeCells}</b>
        </div>
        <div className="kpi danger">
          <CalendarDays className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.overdue")}</span>
          <b>{overdueCells}</b>
        </div>
      </div>

      <div className="chips vaccination-schedule-legend" aria-label={copy(pageContract, "schedule.legend.aria")}>
        {Array.from({ length: 12 }, (_, idx) => idx + 1).map((m) => (
          <Link key={m} href={monthHref(m)} className={m === month ? "chip on" : "chip"} scroll={false} prefetch={false} aria-current={m === month ? "page" : undefined}>
            {monthLabel(year, m)}
          </Link>
        ))}
        {legend.map((item) => (
          <span key={item.key} className="chip">
            <span className={`schedule-dot state-${item.key}`} aria-hidden="true" />
            {item.label}
          </span>
        ))}
      </div>

      {!result.ok ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "18px 16px", flexWrap: "wrap" }}>
          <Layers className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--danger)", flexShrink: 0 }} />
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>{copy(pageContract, "section.full_schedule.unavailable_title")}</b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {copy(pageContract, "section.full_schedule.unavailable_body")} {result.error.message}
            </span>
          </div>
        </div>
      ) : rows.length === 0 || protocols.length === 0 ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "18px 16px", flexWrap: "wrap" }}>
          <Layers className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} />
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>{copy(pageContract, "section.full_schedule.empty_title")}</b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {copy(pageContract, "section.full_schedule.empty_body")}
            </span>
          </div>
        </div>
      ) : (
        <>
          {staleSchedule ? (
            <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "12px 16px", flexWrap: "wrap", borderTop: "1px solid var(--line)" }}>
              <CalendarDays className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--warn)", flexShrink: 0 }} />
              <div style={{ minWidth: 0, flex: 1 }}>
                <b style={{ fontSize: 14 }}>{copy(pageContract, "section.full_schedule.stale_title")}</b>
                <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
                  {copy(pageContract, "section.full_schedule.stale_body")}
                </span>
              </div>
            </div>
          ) : null}
          <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.full_schedule.title")}>
            <table className="full-vaccine-schedule-table" style={{ minWidth: singleScheduleColumn ? 1040 : Math.max(1040, 430 + protocols.length * 142) }}>
              <thead>
                <tr>
                  {fixedColumns.map((column) => (
                    <th key={column.key}>{column.label}</th>
                  ))}
                  {singleScheduleColumn ? (
                    <>
                      <th>{copy(pageContract, "schedule.column.vaccines")}</th>
                      <th>{copy(pageContract, "schedule.column.next_due")}</th>
                      <th>{copy(pageContract, "schedule.column.status")}</th>
                    </>
                  ) : (
                    protocols.map((protocol) => <th key={protocol.protocolId}>{vaccinationDriveDisplayName(protocol.name)}</th>)
                  )}
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const byProtocol = new Map(row.cells.map((cell) => [cell.protocolId, cell]));
                  const wrapperCell = singleScheduleColumn ? byProtocol.get(protocols[0].protocolId) : undefined;
                  const wrapperView = singleScheduleColumn ? scheduleCellView(wrapperCell, year, month, pageContract) : null;
                  return (
                    <tr key={`${row.parkId}-${row.shedId}-${row.stage}-${row.ageBand ?? "all"}`} className="schedule-click-row">
                      <td>
                        {rowLink(row, <ClipText title={row.parkName}>{row.parkName}</ClipText>)}
                      </td>
                      <td>
                        {rowLink(row, <ClipText title={row.shedName} className="strong">{row.shedName}</ClipText>)}
                      </td>
                      <td>
                        {rowLink(row, <ClipText title={rowType(row, pageContract)}>{rowType(row, pageContract)}</ClipText>)}
                      </td>
                      <td className="muted">{rowLink(row, row.animals, "num")}</td>
                      {singleScheduleColumn && wrapperView ? (
                        <>
                          <td>
                            {rowLink(row, <ClipText title={scheduleCellVaccines(wrapperCell, pageContract)}>{scheduleCellVaccines(wrapperCell, pageContract)}</ClipText>)}
                          </td>
                          <td className={`schedule-date-cell state-${wrapperView.state}`} title={wrapperView.title}>
                            {rowLink(row, wrapperView.label, "schedule-date-link")}
                          </td>
                          <td className={`schedule-status-cell state-${wrapperView.state}`} title={wrapperView.title}>
                            {rowLink(
                              row,
                              wrapperView.state !== "no_record" ? <Tag tone={STATE_TONE[wrapperView.state]}>{legend.find((item) => item.key === wrapperView.state)?.label ?? wrapperView.state}</Tag> : wrapperView.label,
                              "schedule-status-link",
                            )}
                          </td>
                        </>
                      ) : (
                        protocols.map((protocol) => {
                          const cell = byProtocol.get(protocol.protocolId);
                          const view = scheduleCellView(cell, year, month, pageContract);
                          return (
                            <td key={protocol.protocolId} className={`schedule-cell state-${view.state}`} title={view.title}>
                              <Link
                                href={detailHref(row, vaccinationScheduleCellAsOf(cell, year))}
                                className="schedule-cell-link"
                                scroll={false}
                                prefetch={false}
                                title={copy(pageContract, "schedule.row.open_title")}
                              >
                                <span>{view.label}</span>
                                {view.state !== "no_record" ? <Tag tone={STATE_TONE[view.state]}>{legend.find((item) => item.key === view.state)?.label ?? view.state}</Tag> : null}
                              </Link>
                            </td>
                          );
                        })
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {nextScheduleHref ? (
            <div className="bd" style={{ padding: "12px 16px", borderTop: "1px solid var(--line)", display: "flex", justifyContent: "flex-end" }}>
              <Link href={nextScheduleHref} className="btn sm" scroll={false} prefetch={false}>
                {copy(pageContract, "section.full_schedule.next_rows")}
              </Link>
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}
