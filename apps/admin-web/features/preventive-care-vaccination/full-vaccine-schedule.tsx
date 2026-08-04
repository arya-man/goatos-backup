import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
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
import { ScheduleMoveDrawer, type ScheduleMoveDrawerRow } from "./full-vaccine-schedule-move-drawer";
import { revalidateVaccinationCommandLenses } from "@/lib/vaccination-command-lenses";

const CURRENT_YEAR = Number(todayIso().slice(0, 4));
const CURRENT_MONTH = Number(todayIso().slice(5, 7));
const MIN_SCHEDULE_YEAR = 2025;
const PAGE_LIMIT = 2000;

type DriveAssignmentRow = VaccinationDriveAssignmentResponse["rows"][number];

type OperatorDayScheduleRow = {
  key: string;
  plannedDate: string;
  originalPlannedDate: string;
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
  vaccineOriginalDates: Record<string, string>;
  capacity: string;
  sheds: Array<{
    id?: string;
    name: string;
    animals: number;
    partitions: Array<{ label: string; animals: number }>;
  }>;
};

function selectedScheduleYear(searchParams: RouteSearchParams | undefined): number {
  return boundedInt(one(searchParams ?? {}, "schedule_year"), CURRENT_YEAR, MIN_SCHEDULE_YEAR, CURRENT_YEAR + 5);
}

function selectedScheduleMonth(searchParams: RouteSearchParams | undefined): number {
  return boundedInt(one(searchParams ?? {}, "schedule_month"), CURRENT_MONTH, 1, 12);
}

function scheduleWindowMonths(): Array<{ year: number; month: number }> {
  const current = new Date(Date.UTC(CURRENT_YEAR, CURRENT_MONTH - 1, 1));
  return [-1, 0, 1].map((delta) => {
    const next = new Date(Date.UTC(current.getUTCFullYear(), current.getUTCMonth() + delta, 1));
    return { year: next.getUTCFullYear(), month: next.getUTCMonth() + 1 };
  });
}

function isScheduleWindowMonth(year: number, month: number): boolean {
  return scheduleWindowMonths().some((item) => item.year === year && item.month === month);
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

function scheduleMoveHref(closeHref: string, row: OperatorDayScheduleRow): string {
  return `${closeHref}#schedule_move=${encodeURIComponent(row.key)}`;
}

function scheduleMoveRedirect(returnTo: string, params: Record<string, string>): string {
  const [pathAndSearch, hash] = returnTo.split("#", 2);
  const base = pathAndSearch.startsWith("/") ? pathAndSearch : "/vaccination?view=schedule";
  const url = new URL(base, "http://mesha.local");
  for (const [key, value] of Object.entries(params)) {
    url.searchParams.set(key, value);
  }
  return `${url.pathname}${url.search}${hash ? `#${hash}` : ""}`;
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

function isoDayNumber(value: string): number {
  const [year, month, day] = value.split("-").map((part) => Number.parseInt(part, 10));
  if (!year || !month || !day) return Number.NaN;
  return Math.floor(Date.UTC(year, month - 1, day) / 86_400_000);
}

function contiguousDateRun(dates: string[], selectedDate: string): string[] {
  const sortedDates = Array.from(new Set(dates)).sort();
  const selectedIndex = sortedDates.indexOf(selectedDate);
  if (selectedIndex < 0) return selectedDate ? [selectedDate] : [];
  let start = selectedIndex;
  while (start > 0 && isoDayNumber(sortedDates[start]) - isoDayNumber(sortedDates[start - 1]) === 1) {
    start -= 1;
  }
  let end = selectedIndex;
  while (end + 1 < sortedDates.length && isoDayNumber(sortedDates[end + 1]) - isoDayNumber(sortedDates[end]) === 1) {
    end += 1;
  }
  return sortedDates.slice(start, end + 1);
}

function moveDrawerRows(rows: OperatorDayScheduleRow[], closeHref: string): ScheduleMoveDrawerRow[] {
  const originalDatesByVaccine = new Map<string, string[]>();
  for (const row of rows) {
    for (const vaccineCode of row.vaccineCodes ?? []) {
      const key = `${row.parkId}|${vaccineCode}`;
      let dates = originalDatesByVaccine.get(key);
      if (!dates) {
        dates = [];
        originalDatesByVaccine.set(key, dates);
      }
      dates.push(row.vaccineOriginalDates?.[vaccineCode] || row.originalPlannedDate || row.plannedDate);
    }
  }
  return rows.map((row) => ({
    eventId: row.key,
    plannedDate: row.plannedDate,
    originalPlannedDate: row.originalPlannedDate,
    operatorName: row.operatorName,
    parkId: row.parkId,
    parkName: row.parkName,
    animals: row.animals,
    totalDoses: row.totalDoses,
    vaccineCodes: row.vaccineCodes,
    vaccineOriginalDates: row.vaccineOriginalDates ?? {},
    vaccineOriginalDateSets: Object.fromEntries(
      (row.vaccineCodes ?? []).map((vaccineCode) => {
        const originalDate = row.vaccineOriginalDates?.[vaccineCode] || row.originalPlannedDate || row.plannedDate;
        return [vaccineCode, contiguousDateRun(originalDatesByVaccine.get(`${row.parkId}|${vaccineCode}`) ?? [], originalDate)];
      }),
    ),
    vaccineNames: row.vaccineNames,
    returnTo: closeHref,
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
        originalPlannedDate: row.originalPlannedDate || row.plannedDate,
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
        vaccineOriginalDates: {},
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
      const originalDate = row.vaccineOriginalDates?.[vaccineCode] || row.originalPlannedDate || row.plannedDate;
      group.vaccineOriginalDates[vaccineCode] = group.vaccineOriginalDates[vaccineCode] || originalDate;
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
  const originalDriveDates = String(formData.get("original_drive_dates") ?? "")
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
  const overrideDate = String(formData.get("override_date") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  const returnTo = String(formData.get("return_to") ?? "").trim();
  if (!parkID || !vaccineCode || !originalDriveDate || !overrideDate) {
    redirect(scheduleMoveRedirect(returnTo, { schedule_move_result: "missing" }));
  }
  const moveDates = Array.from(new Set(originalDriveDates.length > 0 ? originalDriveDates : [originalDriveDate])).sort();
  let result: Awaited<ReturnType<typeof postponeVaccinationDriveDate>> | undefined;
  for (const moveDate of moveDates) {
    // serial-await: allow each original-date slice must stop on the first backend conflict so later slices are not partially moved.
    result = await postponeVaccinationDriveDate({
      park_id: parkID,
      vaccine_code: vaccineCode,
      original_drive_date: moveDate,
      override_date: overrideDate,
      reason,
    });
    if (!result.ok) break;
  }
  revalidateVaccinationCommandLenses();
  if (!result?.ok) {
    redirect(scheduleMoveRedirect(returnTo, {
      schedule_move_result: "error",
      schedule_move_code: result?.error.code ?? result?.error.kind ?? "unknown",
    }));
  }
  redirect(scheduleMoveRedirect(returnTo, {
    schedule_move_result: "recorded",
    schedule_move_vaccine: vaccineCode,
    schedule_move_requested_date: result.data.requested_override_date || overrideDate,
    schedule_move_date: result.data.applied_override_date || result.data.override_date || overrideDate,
    schedule_move_shifted: result.data.auto_shifted ? "1" : "0",
    schedule_move_conflict_vaccine: result.data.conflicting_vaccine_label || result.data.conflicting_vaccine_code || "",
    schedule_move_conflict_date: result.data.conflicting_date || "",
  }));
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
  const requestedYear = selectedScheduleYear(searchParams);
  const requestedMonth = selectedScheduleMonth(searchParams);
  const year = isScheduleWindowMonth(requestedYear, requestedMonth) ? requestedYear : CURRENT_YEAR;
  const month = isScheduleWindowMonth(requestedYear, requestedMonth) ? requestedMonth : CURRENT_MONTH;
  const monthWindow = scheduleWindowMonths();
  const result = scheduleResult ?? (await loadDriveSchedule(scope, year, month));
  const rows = result.ok ? result.data.rows : [];
  const operatorDayRows = groupOperatorDayRows(rows);
  const parks = new Set(rows.map((row) => row.parkId).filter(Boolean));
  const sheds = new Set(rows.map((row) => `${row.parkId}|${row.physicalShed}`).filter(Boolean));
  const animals = rows.reduce((sum, row) => sum + row.animals, 0);
  const closeHref = scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year), schedule_month: String(month) });
  const selectedScheduleEvent = one(searchParams ?? {}, "schedule_event");
  const selectedScheduleMove = one(searchParams ?? {}, "schedule_move");
  const scheduleMoveStatus = one(searchParams ?? {}, "schedule_move_result");
  const scheduleMoveVaccine = one(searchParams ?? {}, "schedule_move_vaccine");
  const scheduleMoveDate = one(searchParams ?? {}, "schedule_move_date");
  const scheduleMoveRequestedDate = one(searchParams ?? {}, "schedule_move_requested_date");
  const scheduleMoveShifted = one(searchParams ?? {}, "schedule_move_shifted") === "1";
  const scheduleMoveConflictVaccine = one(searchParams ?? {}, "schedule_move_conflict_vaccine");
  const scheduleMoveConflictDate = one(searchParams ?? {}, "schedule_move_conflict_date");
  const scheduleDrawerRows = drawerRows(operatorDayRows, pageContract, scope);
  const scheduleMoveRows = moveDrawerRows(operatorDayRows, closeHref);

  function monthHref(nextYear: number, nextMonth: number) {
    return scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(nextYear), schedule_month: String(nextMonth) });
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
        <span className="chip on" aria-current="page">
          {year}
        </span>
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
        {monthWindow.map(({ year: itemYear, month: itemMonth }) => (
          <Link
            key={`${itemYear}-${itemMonth}`}
            href={monthHref(itemYear, itemMonth)}
            className={itemYear === year && itemMonth === month ? "chip on" : "chip"}
            scroll={false}
            prefetch={false}
            aria-current={itemYear === year && itemMonth === month ? "page" : undefined}
          >
            {monthLabel(itemYear, itemMonth)}
          </Link>
        ))}
      </div>

      {scheduleMoveStatus ? (
        <div className={`schedule-move-banner ${scheduleMoveStatus === "recorded" ? "tone-ok" : "tone-danger"}`} role="status">
          <b>{copy(pageContract, scheduleMoveStatus === "recorded" ? "schedule.move.recorded_title" : scheduleMoveStatus === "missing" ? "schedule.move.missing_title" : "schedule.move.error_title")}</b>
          <span>
            {scheduleMoveStatus === "recorded" && scheduleMoveVaccine && scheduleMoveDate
              ? scheduleMoveShifted && scheduleMoveRequestedDate
                ? `Requested ${fmtDate(scheduleMoveRequestedDate)}; scheduled ${fmtDate(scheduleMoveDate)} due to vaccine spacing.${scheduleMoveConflictVaccine && scheduleMoveConflictDate ? ` Too close to ${scheduleMoveConflictVaccine} on ${fmtDate(scheduleMoveConflictDate)}.` : ""}`
                : `${scheduleMoveVaccine} moved to ${fmtDate(scheduleMoveDate)}. ${copy(pageContract, "schedule.move.recorded_body")}`
              : copy(pageContract, scheduleMoveStatus === "recorded" ? "schedule.move.recorded_body" : scheduleMoveStatus === "missing" ? "schedule.move.missing_body" : "schedule.move.error_body")}
          </span>
        </div>
      ) : null}

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
                  <td>
                    <LocalOverlayLink href={scheduleMoveHref(closeHref, row)} className="celllink schedule-status-link" scroll={false}>
                      <span className="btn sm">{copy(pageContract, "schedule.move.open")}</span>
                    </LocalOverlayLink>
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
          <ScheduleMoveDrawer
            rows={scheduleMoveRows}
            initialSelectedEventId={selectedScheduleMove}
            closeHref={closeHref}
            pageContract={pageContract}
            action={postponeDriveDateAction}
          />
        </div>
      )}
    </section>
  );
}
