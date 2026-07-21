import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { CalendarDays, Layers, MapPinned, Warehouse } from "lucide-react";
import {
  getVaccinationSchedule,
  type ApiResult,
  type VaccinationOperationsCell,
  type VaccinationOperationsCohort,
  type VaccinationOperationsCounts,
  type VaccinationOperationsResponse,
} from "@/lib/api/server";
import { copy, optionGroup, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate, todayIso } from "@/lib/format";
import { backendScope, scopeHref, type Scope } from "@/lib/scope";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { ClipText, Tag, type Tone } from "@/components/ui-primitives";
import { ScheduleLocalDrawer, type ScheduleDrawerRow } from "./full-vaccine-schedule-drawer";
import { VaccineChipOverflow } from "./vaccine-chip-overflow";

const CURRENT_YEAR = Number(todayIso().slice(0, 4));
const CURRENT_MONTH = Number(todayIso().slice(5, 7));
const PAGE_LIMIT = 100;
const SHED_CHIP_PREVIEW_LIMIT = 2;
const VACCINE_CHIP_PREVIEW_LIMIT = 2;

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

type ScheduleLoadMetric = {
  key: string;
  label: string;
  value: number;
  tone?: "primary" | "warn";
};

type ScheduleShedGroup = {
  shed: string;
  shedId?: string;
  count: number;
};

type FullScheduleRow = {
  eventId: string;
  date: string;
  parkId: string;
  parkName: string;
  sheds: ScheduleShedGroup[];
  animals: number;
  vaccines: string[];
  counts: VaccinationOperationsCounts;
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

function selectedScheduleEvent(searchParams: RouteSearchParams | undefined): string | undefined {
  const eventId = one(searchParams ?? {}, "schedule_event");
  return eventId && eventId.length <= 256 ? eventId : undefined;
}

function monthLabel(year: number, month: number): string {
  return new Intl.DateTimeFormat("en", { month: "short", year: "numeric", timeZone: "Asia/Kolkata" }).format(new Date(Date.UTC(year, month - 1, 1)));
}

function cellDate(cell: VaccinationOperationsCell): string {
  return (cell.nextDue ?? cell.lastDose ?? "").slice(0, 10);
}

function rowDate(cohort: VaccinationOperationsCohort): string {
  return (cohort.nextDue ?? cohort.lastDose ?? cohort.cells.map(cellDate).find(Boolean) ?? "").slice(0, 10);
}

function rowDriveState(row: FullScheduleRow, pageContract: AdminUiPageContract): ScheduleCellView {
  const { counts } = row;
  const date = row.date;
  const label = date ? fmtDate(date) : copy(pageContract, "label.placeholder");
  if (counts.deferred > 0) {
    return { state: "due_soon", label, title: copy(pageContract, "schedule.state.deferred_title") };
  }
  if (counts.overdue > 0 || counts.missed > 0) {
    return { state: "overdue", label, title: copy(pageContract, "schedule.state.overdue_title") };
  }
  if (counts.due > 0 || counts.inProgress > 0 || counts.proofPending > 0 || counts.rejected > 0) {
    return { state: "due_soon", label, title: copy(pageContract, "schedule.state.due_title") };
  }
  if (counts.total > 0 && counts.accepted >= counts.total) {
    return { state: "up_to_date", label, title: copy(pageContract, "schedule.state.completed_title") };
  }
  return { state: "scheduled", label, title: copy(pageContract, "schedule.state.scheduled_title") };
}

function uniqueSorted(values: Array<string | null | undefined>): string[] {
  return Array.from(new Set(values.map((value) => String(value ?? "").trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b));
}

function countUnit(pageContract: AdminUiPageContract, count: number, singularKey: string, pluralKey: string): string {
  return `${count} ${copy(pageContract, count === 1 ? singularKey : pluralKey)}`;
}

function emptyCounts(): VaccinationOperationsCounts {
  return {
    overdue: 0,
    due: 0,
    inProgress: 0,
    scheduled: 0,
    missed: 0,
    deferred: 0,
    accepted: 0,
    proofPending: 0,
    rejected: 0,
    total: 0,
  };
}

function addCounts(a: VaccinationOperationsCounts, b: VaccinationOperationsCounts): VaccinationOperationsCounts {
  return {
    overdue: a.overdue + b.overdue,
    due: a.due + b.due,
    inProgress: a.inProgress + b.inProgress,
    scheduled: a.scheduled + b.scheduled,
    missed: a.missed + b.missed,
    deferred: a.deferred + b.deferred,
    accepted: a.accepted + b.accepted,
    proofPending: a.proofPending + b.proofPending,
    rejected: a.rejected + b.rejected,
    total: a.total + b.total,
  };
}

function protocolName(protocols: VaccinationOperationsResponse["protocols"], protocolId: string): string | undefined {
  return protocols.find((protocol) => protocol.protocolId === protocolId)?.name;
}

function cellVaccineNames(cell: VaccinationOperationsCell, protocols: VaccinationOperationsResponse["protocols"]): string[] {
  const names = uniqueSorted(cell.vaccineNames ?? []);
  return names.length ? names : uniqueSorted([protocolName(protocols, cell.protocolId)]);
}

function scheduleRows(response: VaccinationOperationsResponse): FullScheduleRow[] {
  const grouped = new Map<string, FullScheduleRow>();
  const shedSeenByGroup = new Map<string, Set<string>>();
  const vaccineSeenByGroup = new Map<string, Set<string>>();

  for (const cohort of response.cohorts) {
    const date = rowDate(cohort);
    if (!date) continue;
    const key = `${date}|${cohort.parkId}`;
    const row = grouped.get(key) ?? {
      eventId: `schedule:${cohort.parkId}:${date}`,
      date,
      parkId: cohort.parkId,
      parkName: cohort.parkName,
      sheds: [],
      animals: 0,
      vaccines: [],
      counts: emptyCounts(),
    };
    grouped.set(key, row);

    row.counts = addCounts(row.counts, cohort.counts);

    let shedSeen = shedSeenByGroup.get(key);
    if (!shedSeen) {
      shedSeen = new Set<string>();
      shedSeenByGroup.set(key, shedSeen);
    }
    if (!shedSeen.has(cohort.shedId)) {
      shedSeen.add(cohort.shedId);
      row.sheds.push({ shed: cohort.shedName, shedId: cohort.shedId, count: cohort.animals });
      row.animals += cohort.animals;
    }

    let vaccineSeen = vaccineSeenByGroup.get(key);
    if (!vaccineSeen) {
      vaccineSeen = new Set<string>();
      vaccineSeenByGroup.set(key, vaccineSeen);
    }
    for (const cell of cohort.cells) {
      for (const vaccine of cellVaccineNames(cell, response.protocols)) {
        vaccineSeen.add(vaccine);
      }
    }
    row.vaccines = Array.from(vaccineSeen).sort((a, b) => a.localeCompare(b));
  }

  return Array.from(grouped.values()).sort((a, b) =>
    [a.date, a.parkName, a.eventId].join("|").localeCompare([b.date, b.parkName, b.eventId].join("|")),
  );
}

function scheduleLoadMetrics(row: FullScheduleRow, pageContract: AdminUiPageContract): ScheduleLoadMetric[] {
  const scheduled = row.counts.scheduled;
  const deferred = row.counts.deferred;
  const totalDoses = row.counts.total || row.animals;
  const scheduledAnimals = scheduled > 0 ? scheduled : row.animals;
  const metrics: ScheduleLoadMetric[] = [
    {
      key: "scheduled",
      label: copy(pageContract, "schedule.load.scheduled_label"),
      value: scheduledAnimals,
      tone: "primary",
    },
    {
      key: "doses",
      label: copy(pageContract, "schedule.load.doses_label"),
      value: totalDoses,
      tone: "primary",
    },
  ];
  if (deferred > 0) {
    metrics.push({
      key: "deferred",
      label: copy(pageContract, "schedule.load.deferred_label"),
      value: deferred,
      tone: "warn",
    });
  }
  return metrics;
}

function dateEyebrow(date: string): string {
  if (!date) {
    return "";
  }
  return new Intl.DateTimeFormat("en", { weekday: "short", timeZone: "Asia/Kolkata" }).format(new Date(`${date}T00:00:00+05:30`));
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
  const selectedEventId = selectedScheduleEvent(searchParams);
  const result = scheduleResult ?? (await loadFullSchedule(scope, year, month, cursor));
  const rows = result.ok ? scheduleRows(result.data) : [];
  const uniqueParks = new Set(rows.map((row) => row.parkId).filter(Boolean));
  const totalSheds = rows.reduce((sum, row) => sum + row.sheds.length, 0);
  const totalAnimals = rows.reduce((sum, row) => sum + row.animals, 0);
  const overdueDrives = rows.filter((row) => rowDriveState(row, pageContract).state === "overdue").length;
  const scheduleTable = table(pageContract, "full-vaccine-schedule");
  const fixedColumns = scheduleTable.columns.filter((column) => column.visible);
  const legend = optionGroup(pageContract, "schedule_status_legend");
  const currentScheduleHref = scopeHref("/vaccination", scope, {}, {
    view: "schedule",
    schedule_year: String(year),
    schedule_month: String(month),
    schedule_cursor: cursor,
  });
  const staleSchedule = Boolean(result.ok && (result.data.freshness?.stale || result.data.freshness?.rebuildRequired));
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

  function shedDrawerHref(row: FullScheduleRow): string {
    return `${currentScheduleHref}#schedule_event=${encodeURIComponent(row.eventId)}`;
  }

  const drawerRows: ScheduleDrawerRow[] = rows.map((row) => {
    const date = row.date;
    const drawerHref = shedDrawerHref(row);
    return {
      eventId: row.eventId,
      date,
      parkName: row.parkName || copy(pageContract, "label.placeholder"),
      totalSheds: row.sheds.length,
      totalAnimals: row.animals,
      vaccines: row.vaccines,
      sheds: row.sheds.map((group) => ({
        label: group.shed,
        count: group.count,
        href: group.shedId
          ? scopeHref(
              `/vaccination/execution/sheds/${encodeURIComponent(group.shedId)}`,
              scope,
              { mode: "park", park: row.parkId ?? scope.parkId },
              {
                drive_due_date: date || undefined,
                ret: drawerHref,
              },
            ) + "#animals"
          : undefined,
      })),
    };
  });

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
          <b>{totalSheds}</b>
        </div>
        <div className="kpi">
          <Layers className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.animals")}</span>
          <b>{totalAnimals}</b>
        </div>
        <div className="kpi danger">
          <CalendarDays className="ic" aria-hidden="true" />
          <span>{copy(pageContract, "schedule.kpi.overdue_drives")}</span>
          <b>{overdueDrives}</b>
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
      ) : rows.length === 0 ? (
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
            <table className="full-vaccine-schedule-table">
              <thead>
                <tr>
                  <th>{fixedColumns.find((column) => column.key === "date")?.label ?? copy(pageContract, "schedule.column.date")}</th>
                  <th>{fixedColumns.find((column) => column.key === "park")?.label ?? copy(pageContract, "schedule.column.park")}</th>
                  <th>{copy(pageContract, "schedule.column.sheds")}</th>
                  <th>{copy(pageContract, "schedule.column.workload")}</th>
                  <th>{copy(pageContract, "schedule.column.vaccines")}</th>
                  <th>{copy(pageContract, "schedule.column.status")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const view = rowDriveState(row, pageContract);
                  const vaccines = row.vaccines;
                  const sheds = uniqueSorted(row.sheds.map((shed) => shed.shed));
                  const shedCount = row.sheds.length;
                  const statusLabel = legend.find((item) => item.key === view.state)?.label ?? view.state;
                  const date = row.date;
                  const shedTitle = sheds.join(", ");
                  const previewSheds = sheds.slice(0, SHED_CHIP_PREVIEW_LIMIT);
                  const hiddenShedCount = Math.max(0, shedCount - previewSheds.length);
                  const vaccineTitle = vaccines.join(", ");
                  const previewVaccines = vaccines.slice(0, VACCINE_CHIP_PREVIEW_LIMIT);
                  const hiddenVaccineCount = Math.max(0, vaccines.length - previewVaccines.length);
                  const loadMetrics = scheduleLoadMetrics(row, pageContract);
                  return (
                    <tr key={row.eventId} className="schedule-click-row">
                      <td className={`schedule-date-cell state-${view.state}`} title={view.title}>
                        <LocalOverlayLink href={shedDrawerHref(row)} className="celllink schedule-date-link">
                          <span className="schedule-date-stack">
                            <span className="schedule-date-main">{view.label}</span>
                            <span className="schedule-date-sub">{dateEyebrow(date)}</span>
                          </span>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={shedDrawerHref(row)} className="celllink">
                          <ClipText title={row.parkName}>{row.parkName || copy(pageContract, "label.placeholder")}</ClipText>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={shedDrawerHref(row)} className="celllink schedule-wrap-link schedule-shed-cell" title={shedTitle || copy(pageContract, "schedule.drawer.open_sheds")}>
                          <span className="schedule-chip-list schedule-chip-list-compact">
                            <span className="schedule-mini-chip schedule-count-chip">{countUnit(pageContract, shedCount, "schedule.unit.shed", "schedule.unit.sheds")}</span>
                            {previewSheds.map((shed) => (
                              <span key={shed} className="schedule-mini-chip">{shed}</span>
                            ))}
                            {hiddenShedCount > 0 ? (
                              <span className="schedule-mini-chip schedule-more-chip">+{hiddenShedCount} {copy(pageContract, "schedule.drawer.more")}</span>
                            ) : null}
                          </span>
                        </LocalOverlayLink>
                      </td>
                      <td className="muted num">
                        <LocalOverlayLink
                          href={shedDrawerHref(row)}
                          className="celllink num schedule-animals-link"
                          title={loadMetrics.map((metric) => `${metric.label}: ${metric.value}`).join(" · ")}
                        >
                          <span className="schedule-load-card" aria-label={loadMetrics.map((metric) => `${metric.value} ${metric.label}`).join(", ")}>
                            {loadMetrics.map((metric) => (
                              <span key={metric.key} className={`schedule-load-metric${metric.tone ? ` tone-${metric.tone}` : ""}`}>
                                <span className="schedule-load-value">{metric.value}</span>
                                <span className="schedule-load-label">{metric.label}</span>
                              </span>
                            ))}
                          </span>
                        </LocalOverlayLink>
                      </td>
                      <td className="schedule-vaccine-cell">
                        {hiddenVaccineCount > 0 ? (
                          <VaccineChipOverflow
                            vaccines={vaccines}
                            previewLimit={VACCINE_CHIP_PREVIEW_LIMIT}
                            moreLabel={copy(pageContract, "schedule.drawer.more")}
                            lessLabel={copy(pageContract, "schedule.drawer.less")}
                            ariaLabel={`${copy(pageContract, "schedule.column.vaccines")}: ${vaccineTitle}`}
                          />
                        ) : (
                          <div className="celllink schedule-wrap-link" title={vaccineTitle}>
                            <span className="schedule-chip-list vaccine-chip-list schedule-chip-list-compact">
                              {vaccines.length > 0 ? previewVaccines.map((vaccine) => (
                                <span key={vaccine} className="schedule-mini-chip vaccine-chip">{vaccine}</span>
                              )) : <span className="schedule-mini-chip">{copy(pageContract, "label.placeholder")}</span>}
                            </span>
                          </div>
                        )}
                      </td>
                      <td className={`schedule-status-cell state-${view.state}`} title={view.title}>
                        <LocalOverlayLink href={shedDrawerHref(row)} className="celllink schedule-status-link">
                          <Tag tone={STATE_TONE[view.state]}>{statusLabel}</Tag>
                        </LocalOverlayLink>
                      </td>
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
          <ScheduleLocalDrawer
            rows={drawerRows}
            initialSelectedEventId={selectedEventId}
            closeHref={currentScheduleHref}
            pageContract={pageContract}
          />
        </>
      )}
    </section>
  );
}
