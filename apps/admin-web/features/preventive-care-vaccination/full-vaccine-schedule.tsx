import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { revalidatePath } from "next/cache";
import { CalendarDays, Layers, MapPinned, Warehouse } from "lucide-react";
import {
  getVaccinationDriveAssignments,
  postponeVaccinationDriveDate,
  type ApiResult,
  type VaccinationDriveAssignmentResponse,
} from "@/lib/api/server";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate, todayIso } from "@/lib/format";
import { backendScope, scopeHref, type Scope } from "@/lib/scope";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { ClipText, Tag } from "@/components/ui-primitives";
import { scheduleLoadBuckets, type ScheduleLoadBucket } from "./full-vaccine-schedule-load";
import { ScheduleLocalDrawer, type ScheduleDrawerRow } from "./full-vaccine-schedule-drawer";

const CURRENT_YEAR = Number(todayIso().slice(0, 4));
const CURRENT_MONTH = Number(todayIso().slice(5, 7));
const PAGE_LIMIT = 2000;

type DriveAssignmentRow = VaccinationDriveAssignmentResponse["rows"][number];

type OperatorDayScheduleRow = {
  key: string;
  plannedDate: string;
  operatorId: string;
  operatorName: string;
  parkId: string;
  parkName: string;
  animals: number;
  dueAnimals: number;
  doneAnimals: number;
  deferredAnimals: number;
  overdueAnimals: number;
  totalDoses: number;
  vaccineNames: string[];
  vaccineCodes: string[];
  capacity: string;
  sheds: Array<{
    id?: string;
    name: string;
    animals: number;
    partitions: Array<{ label: string; animals: number }>;
  }>;
};

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

function shedPartitionTitle(pageContract: AdminUiPageContract, shed: OperatorDayScheduleRow["sheds"][number]): string {
  const partitions = shed.partitions.map((partition) => `${partitionLabel(pageContract, partition.label)} ${partition.animals}`);
  return partitions.length > 0 ? `${shed.name}: ${partitions.join(", ")}` : shed.name;
}

function capacityRank(status: string): number {
  if (status === "over_cap_required" || status === "capacity_breach") return 3;
  if (status === "capacity_action") return 2;
  return 1;
}

function strongerCapacity(left: string, right: string): string {
  return capacityRank(right) > capacityRank(left) ? right : left;
}

function workloadTone(capacity: string): string {
  if (capacityRank(capacity) >= 3) return "tone-danger";
  if (capacity === "capacity_action") return "tone-warn";
  return "tone-ok";
}

function workloadSegmentWidth(bucket: ScheduleLoadBucket, total: number): string {
  const percentage = Math.round((Math.max(0, bucket.value) / Math.max(1, total)) * 100);
  return `${Math.max(2, Math.min(100, percentage))}%`;
}

function workloadBucketLabel(pageContract: AdminUiPageContract, key: ScheduleLoadBucket["key"]): string {
  if (key === "scheduled") return copy(pageContract, "schedule.load.scheduled_short");
  if (key === "deferred") return copy(pageContract, "schedule.load.deferred_short");
  if (key === "overdue") return copy(pageContract, "label.overdue");
  return copy(pageContract, "label.done");
}

function scheduleDrawerHref(closeHref: string, row: OperatorDayScheduleRow): string {
  return `${closeHref}#schedule_event=${encodeURIComponent(row.key)}`;
}

function drawerRows(rows: OperatorDayScheduleRow[], pageContract: AdminUiPageContract, scope: Scope): ScheduleDrawerRow[] {
  return rows.map((row) => ({
    eventId: row.key,
    date: row.plannedDate,
    parkName: row.parkName,
    totalSheds: row.sheds.length,
    totalAnimals: row.animals,
    vaccines: row.vaccineNames,
    sheds: row.sheds.map((shed) => {
      const href = shed.id
        ? scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(shed.id)}`, scope, { mode: "park", park: row.parkId })
        : undefined;
      return {
        label: shedPartitionTitle(pageContract, shed),
        count: shed.animals,
        href,
      };
    }),
  }));
}

function groupOperatorDayRows(rows: DriveAssignmentRow[]): OperatorDayScheduleRow[] {
  const groups = new Map<string, OperatorDayScheduleRow>();

  for (const row of rows) {
    const key = `${row.plannedDate}|${row.operatorId}|${row.parkId}`;
    let group = groups.get(key);
    if (!group) {
      group = {
        key,
        plannedDate: row.plannedDate,
        operatorId: row.operatorId,
        operatorName: row.operatorName,
        parkId: row.parkId,
        parkName: row.parkName,
        animals: 0,
        dueAnimals: 0,
        doneAnimals: 0,
        deferredAnimals: 0,
        overdueAnimals: 0,
        totalDoses: 0,
        vaccineNames: [],
        vaccineCodes: [],
        capacity: row.capacity,
        sheds: [],
      };
      groups.set(key, group);
    }
    group = groups.get(key);
    if (!group) continue;
    group.animals += row.animals;
    group.dueAnimals += row.dueAnimals;
    group.doneAnimals += row.doneAnimals;
    group.deferredAnimals += row.deferredAnimals;
    group.overdueAnimals += row.overdueAnimals;
    group.totalDoses += row.totalDoses;
    for (const vaccineName of row.vaccineNames) {
      if (!group.vaccineNames.includes(vaccineName)) {
        group.vaccineNames.push(vaccineName);
      }
    }
    for (const vaccineCode of row.vaccineCodes ?? []) {
      if (!group.vaccineCodes.includes(vaccineCode)) {
        group.vaccineCodes.push(vaccineCode);
      }
    }
    group.capacity = strongerCapacity(group.capacity, row.capacity);

    let shed: OperatorDayScheduleRow["sheds"][number] | undefined = group.sheds.find((item) => item.name === row.physicalShed);
    if (!shed) {
      shed = { id: row.shedId ?? undefined, name: row.physicalShed, animals: 0, partitions: [] };
      group.sheds.push(shed);
    }
    shed.animals += row.animals;
    shed.partitions.push({ label: row.partitionLabel, animals: row.animals });
  }

  return Array.from(groups.values()).sort((a, b) => {
    const dateOrder = a.plannedDate.localeCompare(b.plannedDate);
    if (dateOrder !== 0) return dateOrder;
    return a.operatorName.localeCompare(b.operatorName);
  });
}

async function postponeDriveDateAction(formData: FormData) {
  "use server";
  const parkID = String(formData.get("park_id") ?? "").trim();
  const vaccineCode = String(formData.get("vaccine_code") ?? "").trim();
  const originalDriveDate = String(formData.get("original_drive_date") ?? "").trim();
  const overrideDate = String(formData.get("override_date") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  if (!parkID || !vaccineCode || !originalDriveDate || !overrideDate) return;
  await postponeVaccinationDriveDate({
    park_id: parkID,
    vaccine_code: vaccineCode,
    original_drive_date: originalDriveDate,
    override_date: overrideDate,
    reason,
  });
  revalidatePath("/vaccination");
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
                <th>{copy(pageContract, "schedule.column.vaccines")}</th>
                <th>{copy(pageContract, "schedule.column.workload")}</th>
                <th>{copy(pageContract, "schedule.column.capacity")}</th>
                <th>{copy(pageContract, "schedule.column.postpone")}</th>
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 5 }, (_, row) => (
                <tr key={row}>
                  {Array.from({ length: 8 }, (_, col) => (
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
  const operatorDayRows = groupOperatorDayRows(rows);
  const parks = new Set(rows.map((row) => row.parkId).filter(Boolean));
  const sheds = new Set(rows.map((row) => `${row.parkId}|${row.physicalShed}`).filter(Boolean));
  const animals = rows.reduce((sum, row) => sum + row.animals, 0);
  const closeHref = scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year), schedule_month: String(month) });
  const selectedScheduleEvent = one(searchParams ?? {}, "schedule_event");
  const scheduleDrawerRows = drawerRows(operatorDayRows, pageContract, scope);

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
          <b>{operatorDayRows.length}</b>
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
                <th>{copy(pageContract, "schedule.column.sheds")}</th>
                <th>{copy(pageContract, "schedule.column.vaccines")}</th>
                <th>{copy(pageContract, "schedule.column.workload")}</th>
                <th>{copy(pageContract, "schedule.column.capacity")}</th>
                <th>{copy(pageContract, "schedule.column.postpone")}</th>
              </tr>
            </thead>
            <tbody>
              {operatorDayRows.map((row) => {
                const drawerHref = scheduleDrawerHref(closeHref, row);
                const load = scheduleLoadBuckets({
                  total: row.animals,
                  scheduled: row.dueAnimals,
                  due: 0,
                  inProgress: 0,
                  deferred: row.deferredAnimals,
                  overdue: row.overdueAnimals,
                  missed: 0,
                }, row.animals);
                const visibleBuckets = load.buckets.filter((bucket) => bucket.value > 0);
                const segmentTotal = visibleBuckets.reduce((sum, bucket) => sum + bucket.value, 0) || load.total || row.animals || 1;
                return (
                <tr key={row.key} className="schedule-click-row">
                  <td className="schedule-date-cell state-scheduled">
                    <LocalOverlayLink href={drawerHref} className="celllink schedule-date-link" scroll={false}>
                      <span className="schedule-date-stack">
                      <span className="schedule-date-main">{fmtDate(row.plannedDate)}</span>
                      <span className="schedule-date-sub">{dateEyebrow(row.plannedDate)}</span>
                    </span>
                    </LocalOverlayLink>
                  </td>
                  <td><LocalOverlayLink href={drawerHref} className="celllink" scroll={false}><b>{row.operatorName}</b></LocalOverlayLink></td>
                  <td><LocalOverlayLink href={drawerHref} className="celllink" scroll={false}><ClipText title={row.parkName}>{row.parkName}</ClipText></LocalOverlayLink></td>
                  <td className="schedule-shed-cell">
                    <LocalOverlayLink href={drawerHref} className="celllink schedule-wrap-link" scroll={false} title={row.sheds.map((shed) => shedPartitionTitle(pageContract, shed)).join(", ")}>
                      <span className="operator-day-sheds">
                        {row.sheds.map((shed) => (
                          <span key={shed.name} className="operator-day-shed" title={shedPartitionTitle(pageContract, shed)}>
                            <b>{shed.name}</b>
                            <span className="muted">{shed.animals}</span>
                          </span>
                        ))}
                      </span>
                    </LocalOverlayLink>
                  </td>
                  <td>
                    <LocalOverlayLink href={drawerHref} className="celllink schedule-wrap-link" scroll={false}>
                    <span className="operator-day-vaccines">
                      {row.vaccineNames.map((vaccineName) => (
                        <Tag key={vaccineName} tone="teal" title={vaccineName}>{vaccineName}</Tag>
                      ))}
                    </span>
                    </LocalOverlayLink>
                  </td>
                  <td>
                    <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                    <span className="schedule-load-card operator-workload-card">
                      <span className="schedule-load-head">
                        <span className="schedule-load-total">{row.animals}</span>
                        <span className="schedule-load-total-label">{copy(pageContract, "schedule.unit.animals")}</span>
                        <span className="schedule-load-goats">{row.totalDoses} {copy(pageContract, "schedule.unit.doses")}</span>
                      </span>
                      <span className="schedule-load-bar" aria-hidden="true">
                        {visibleBuckets.length > 0 ? visibleBuckets.map((bucket) => (
                          <span
                            key={bucket.key}
                            className={`schedule-load-seg tone-${bucket.tone}`}
                            style={{ flexBasis: workloadSegmentWidth(bucket, segmentTotal) }}
                          />
                        )) : (
                          <span className={`schedule-load-seg ${workloadTone(row.capacity)}`} style={{ flexBasis: "100%" }} />
                        )}
                      </span>
                      <span className="schedule-load-legend" aria-label={copy(pageContract, "schedule.column.workload")}>
                        {visibleBuckets.map((bucket) => (
                          <span key={bucket.key} className={`schedule-load-item tone-${bucket.tone}`}>
                            <span className="schedule-load-dot" aria-hidden="true" />
                            <span className="schedule-load-count">{bucket.value}</span>
                            <span className="schedule-load-name">{workloadBucketLabel(pageContract, bucket.key)}</span>
                          </span>
                        ))}
                      </span>
                    </span>
                    </LocalOverlayLink>
                  </td>
                  <td><LocalOverlayLink href={drawerHref} className="celllink schedule-status-link" scroll={false}><Tag tone={capacityRank(row.capacity) >= 3 ? "dng" : row.capacity === "capacity_action" ? "warn" : "ok"}>{row.capacity}</Tag></LocalOverlayLink></td>
                  <td>
                    <form action={postponeDriveDateAction} className="schedule-postpone-form">
                      <input type="hidden" name="park_id" value={row.parkId} />
                      <input type="hidden" name="original_drive_date" value={row.plannedDate} />
                      <input type="hidden" name="reason" value={copy(pageContract, "schedule.postpone.reason_default")} />
                      <select name="vaccine_code" aria-label={copy(pageContract, "schedule.postpone.vaccine")} required>
                        {row.vaccineCodes.map((code) => (
                          <option key={code} value={code}>{code}</option>
                        ))}
                      </select>
                      <input name="override_date" aria-label={copy(pageContract, "schedule.postpone.new_date")} type="date" min={row.plannedDate} required />
                      <button className="btn sm" type="submit">{copy(pageContract, "schedule.postpone.action")}</button>
                    </form>
                  </td>
                </tr>
                );
              })}
            </tbody>
          </table>
          <ScheduleLocalDrawer
            rows={scheduleDrawerRows}
            initialSelectedEventId={selectedScheduleEvent}
            closeHref={closeHref}
            pageContract={pageContract}
          />
        </div>
      )}
    </section>
  );
}
