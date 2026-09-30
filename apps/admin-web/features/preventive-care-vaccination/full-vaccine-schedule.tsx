import type { ReactNode } from "react";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import MuiLink from "@mui/material/Link";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import Typography from "@mui/material/Typography";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
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
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { DividedStack } from "@/components/app/divided-stack";
import { EmptyState } from "@/components/app/empty-state";
import { LinkButton } from "@/components/app/link-button";
import { SegmentTabs } from "@/components/app/list/segment-tabs";
import { InvoiceAnalytic } from "@/components/app/sections/invoice/invoice-analytic";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { scheduleLoadBuckets, type ScheduleLoadBucket } from "./full-vaccine-schedule-load";
import { ScheduleLocalDrawer, type ScheduleDrawerRow } from "./full-vaccine-schedule-drawer";
import { ScheduleMoveDrawer, type ScheduleMoveDrawerRow } from "./full-vaccine-schedule-move-drawer";
import { HashSectionScroller } from "./hash-section-scroller";
import { FULL_SCHEDULE } from "./command-board-layout";
import { revalidateVaccinationCommandLenses } from "@/lib/vaccination-command-lenses";
import { addSchedulePen, schedulePenKey, type SchedulePen } from "./full-vaccine-schedule-pens.ts";

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
  pens: SchedulePen[];
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

function penTitle(pen: SchedulePen): string {
  return `${pen.display} ${pen.animals}`;
}

function capacityRank(status: string): number {
  if (status === "over_cap_required" || status === "capacity_breach") return 3;
  if (status === "capacity_action") return 2;
  return 1;
}

function strongerCapacity(left: string, right: string): string {
  return capacityRank(right) > capacityRank(left) ? right : left;
}

/** Workload bucket tone -> theme palette colour (the bar segments and legend dots). */
const LOAD_TONE_COLOR: Record<ScheduleLoadBucket["tone"], string> = {
  ok: "success.main",
  warn: "warning.main",
  danger: "error.main",
  done: "grey.500",
};

function workloadTone(capacity: string): string {
  if (capacityRank(capacity) >= 3) return LOAD_TONE_COLOR.danger;
  if (capacity === "capacity_action") return LOAD_TONE_COLOR.warn;
  return LOAD_TONE_COLOR.ok;
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

function drawerRows(rows: OperatorDayScheduleRow[], scope: Scope, closeHref: string): ScheduleDrawerRow[] {
  return rows.map((row) => ({
    eventId: row.key,
    date: row.plannedDate,
    parkName: row.parkName,
    totalSheds: row.pens.length,
    totalAnimals: row.animals,
    vaccines: row.vaccineNames,
    sheds: row.pens.map((pen) => {
      const ret = scheduleDrawerHref(closeHref, row);
      const href = pen.shedId
        ? scopeHref(
            `/vaccination/execution/sheds/${encodeURIComponent(pen.shedId)}`,
            scope,
            { mode: "park", park: row.parkId },
            { partition_label: pen.partitionLabel, ret },
          )
        : undefined;
      return {
        label: pen.display,
        count: pen.animals,
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
        pens: [],
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

    addSchedulePen(group.pens, row, row.animals);
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
  const rows = result.ok ? listOrEmpty(result.data.rows) : [];
  const operatorDayRows = groupOperatorDayRows(rows);
  const parks = new Set(rows.map((row) => row.parkId).filter(Boolean));
  const sheds = new Set(rows.map((row) => schedulePenKey(row)));
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
  const scheduleDrawerRows = drawerRows(operatorDayRows, scope, closeHref);
  const scheduleMoveRows = moveDrawerRows(operatorDayRows, closeHref);

  function monthHref(nextYear: number, nextMonth: number) {
    return scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(nextYear), schedule_month: String(nextMonth) });
  }

  const headCells = [
    { id: "date", label: copy(pageContract, "schedule.column.date") },
    { id: "operator", label: copy(pageContract, "schedule.column.operator") },
    { id: "park", label: copy(pageContract, "schedule.column.park") },
    { id: "sheds", label: copy(pageContract, "schedule.column.sheds") },
    { id: "vaccines", label: copy(pageContract, "schedule.column.vaccines") },
    { id: "workload", label: copy(pageContract, "schedule.column.workload") },
    { id: "postpone", label: copy(pageContract, "schedule.column.postpone") },
  ];
  const summaryCells = [
    { key: "parks", title: copy(pageContract, "schedule.kpi.parks"), value: parks.size, icon: "mingcute:location-fill" as const, color: "info.main" },
    { key: "sheds", title: copy(pageContract, "schedule.kpi.sheds"), value: sheds.size, icon: "solar:home-angle-bold-duotone" as const, color: "primary.main" },
    { key: "animals", title: copy(pageContract, "schedule.kpi.animals_assigned"), value: animals, icon: "solar:users-group-rounded-bold-duotone" as const, color: "warning.main" },
    { key: "drive_rows", title: copy(pageContract, "schedule.kpi.drive_rows"), value: operatorDayRows.length, icon: "solar:calendar-date-bold" as const, color: "success.main" },
  ];

  return (
    // Template list card (sections/invoice list view): CardHeader + action, the InvoiceAnalytic summary
    // strip, the month segment tabs, then the operator-day table in the template Scrollbar.
    <Card id="full-schedule" sx={{ scrollMarginTop: "calc(10 * var(--spacing))" }}>
      <HashSectionScroller id="full-schedule" />
      <CardHeader
        title={copy(pageContract, "section.full_schedule.operator_title")}
        subheader={copy(pageContract, "section.full_schedule.operator_note")}
        action={
          <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
            <Label color="primary" aria-current="page">{year}</Label>
            <LinkButton href={scopeHref("/vaccination", scope)} scroll={false} size="small" variant="outlined" color="inherit">
              {copy(pageContract, "action.open_shed_board")}
            </LinkButton>
          </Stack>
        }
      />

      <Scrollbar sx={{ minHeight: FULL_SCHEDULE.stripMinHeight }}>
        <DividedStack dividerOrientation="vertical" direction="row" sx={{ py: 2 }}>
          {summaryCells.map((cell) => (
            <InvoiceAnalytic
              key={cell.key}
              title={cell.title}
              total={cell.value}
              percent={100}
              icon={cell.icon}
              color={cell.color}
            />
          ))}
        </DividedStack>
      </Scrollbar>

      <Box sx={{ px: 2.5, py: 2 }}>
        <SegmentTabs
          keepScroll
          ariaLabel={copy(pageContract, "schedule.legend.aria")}
          value={`${year}-${month}`}
          tabs={monthWindow.map(({ year: itemYear, month: itemMonth }) => ({
            value: `${itemYear}-${itemMonth}`,
            label: monthLabel(itemYear, itemMonth),
            href: monthHref(itemYear, itemMonth),
          }))}
        />
      </Box>

      {scheduleMoveStatus ? (
        <Alert severity={scheduleMoveStatus === "recorded" ? "success" : "error"} role="status" sx={{ mx: 2.5, mb: 2 }}>
          <AlertTitle>{copy(pageContract, scheduleMoveStatus === "recorded" ? "schedule.move.recorded_title" : scheduleMoveStatus === "missing" ? "schedule.move.missing_title" : "schedule.move.error_title")}</AlertTitle>
          {scheduleMoveStatus === "recorded" && scheduleMoveVaccine && scheduleMoveDate
            ? scheduleMoveShifted && scheduleMoveRequestedDate
              ? `Requested ${fmtDate(scheduleMoveRequestedDate)}; scheduled ${fmtDate(scheduleMoveDate)} due to vaccine spacing.${scheduleMoveConflictVaccine && scheduleMoveConflictDate ? ` Too close to ${scheduleMoveConflictVaccine} on ${fmtDate(scheduleMoveConflictDate)}.` : ""}`
              : `${scheduleMoveVaccine} moved to ${fmtDate(scheduleMoveDate)}. ${copy(pageContract, "schedule.move.recorded_body")}`
            : copy(pageContract, scheduleMoveStatus === "recorded" ? "schedule.move.recorded_body" : scheduleMoveStatus === "missing" ? "schedule.move.missing_body" : "schedule.move.error_body")}
        </Alert>
      ) : null}

      {!result.ok ? (
        <Alert severity="error" sx={{ mx: 2.5, mb: 3 }}>
          <AlertTitle>{copy(pageContract, "section.full_schedule.assignment_unavailable_title")}</AlertTitle>
          {result.error.message}
        </Alert>
      ) : rows.length === 0 ? (
        <EmptyState
          title={copy(pageContract, "section.full_schedule.no_assignments_title")}
          description={`${copy(pageContract, "section.full_schedule.no_assignments_body")} ${monthLabel(year, month)}.`}
          sx={{ mx: 2.5, mb: 3 }}
        />
      ) : (
        <>
          <Scrollbar>
            <Table aria-label={copy(pageContract, "section.full_schedule.operator_title")} sx={{ minWidth: FULL_SCHEDULE.tableMinWidth }}>
              <TableHeadCustom headCells={headCells} />
              <TableBody>
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
                    <TableRow key={row.key} hover>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>
                        <DrawerLink href={drawerHref}>
                          <Typography variant="subtitle2" component="span" sx={{ display: "block" }}>{fmtDate(row.plannedDate)}</Typography>
                          <Typography variant="caption" component="span" sx={{ display: "block", color: "text.secondary" }}>{dateEyebrow(row.plannedDate)}</Typography>
                        </DrawerLink>
                      </TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>
                        <DrawerLink href={drawerHref}><Typography variant="subtitle2" component="span">{row.operatorName}</Typography></DrawerLink>
                      </TableCell>
                      <TableCell sx={{ maxWidth: 180 }}>
                        <DrawerLink href={drawerHref}><Typography variant="body2" component="span" noWrap title={row.parkName} sx={{ display: "block" }}>{row.parkName}</Typography></DrawerLink>
                      </TableCell>
                      <TableCell sx={{ minWidth: 240, maxWidth: 360 }}>
                        <DrawerLink href={drawerHref} title={row.pens.map(penTitle).join(", ")}>
                          <Stack direction="row" useFlexGap sx={{ flexWrap: "wrap", gap: 0.75 }}>
                            {row.pens.map((pen) => (
                              <Label key={pen.key} title={penTitle(pen)}>
                                {pen.display}
                                <Box component="span" sx={{ ml: 0.5, color: "text.secondary" }}>{pen.animals}</Box>
                              </Label>
                            ))}
                          </Stack>
                        </DrawerLink>
                      </TableCell>
                      <TableCell sx={{ minWidth: 120 }}>
                        <DrawerLink href={drawerHref}>
                          <Stack direction="row" useFlexGap sx={{ flexWrap: "wrap", gap: 0.75 }}>
                            {row.vaccineNames.map((vaccineName) => (
                              <Label key={vaccineName} color="info" title={vaccineName}>{vaccineName}</Label>
                            ))}
                          </Stack>
                        </DrawerLink>
                      </TableCell>
                      <TableCell sx={{ minWidth: 200 }}>
                        <DrawerLink href={drawerHref}>
                          {/* Operator workload: animals + doses, the animal-based bucket bar, then the bucket legend. */}
                          <Stack spacing={0.75} sx={{ maxWidth: 260 }}>
                            <Stack direction="row" spacing={0.75} sx={{ alignItems: "baseline" }}>
                              <Typography variant="subtitle1" component="span">{row.animals}</Typography>
                              <Typography variant="caption" component="span" sx={{ color: "text.secondary" }}>{copy(pageContract, "schedule.unit.animals")}</Typography>
                              <Typography variant="caption" component="span" sx={{ ml: "auto", color: "text.secondary", whiteSpace: "nowrap" }}>{row.totalDoses} {copy(pageContract, "schedule.unit.doses")}</Typography>
                            </Stack>
                            <Stack direction="row" spacing={0.125} aria-hidden="true" sx={{ height: "calc(1 * var(--spacing))", borderRadius: 1, overflow: "hidden", bgcolor: "action.hover" }}>
                              {visibleBuckets.length > 0 ? visibleBuckets.map((bucket) => (
                                <Box key={bucket.key} component="span" sx={{ flexBasis: workloadSegmentWidth(bucket, segmentTotal), minWidth: 2, bgcolor: LOAD_TONE_COLOR[bucket.tone] }} />
                              )) : (
                                <Box component="span" sx={{ flexBasis: "100%", bgcolor: workloadTone(row.capacity) }} />
                              )}
                            </Stack>
                            <Stack direction="row" useFlexGap aria-label={copy(pageContract, "schedule.column.workload")} sx={{ flexWrap: "wrap", columnGap: 1.25, rowGap: 0.5 }}>
                              {visibleBuckets.map((bucket) => (
                                <Stack key={bucket.key} component="span" direction="row" spacing={0.5} sx={{ alignItems: "center", typography: "caption" }}>
                                  <Box component="span" aria-hidden="true" sx={{ width: "calc(1 * var(--spacing))", height: "calc(1 * var(--spacing))", borderRadius: 0.75, flexShrink: 0, bgcolor: LOAD_TONE_COLOR[bucket.tone] }} />
                                  <Box component="span" sx={{ fontWeight: "fontWeightSemiBold" }}>{bucket.value}</Box>
                                  <Box component="span" sx={{ color: "text.secondary" }}>{workloadBucketLabel(pageContract, bucket.key)}</Box>
                                </Stack>
                              ))}
                            </Stack>
                          </Stack>
                        </DrawerLink>
                      </TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>
                        <Button
                          component={LocalOverlayLink}
                          href={scheduleMoveHref(closeHref, row)}
                          scroll={false}
                          aria-haspopup="dialog"
                          size="small"
                          variant="outlined"
                          color="inherit"
                        >
                          {copy(pageContract, "schedule.move.open")}
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Scrollbar>
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
        </>
      )}
    </Card>
  );
}

/** A table cell's whole content opens the operator-day drawer (template `Link component={RouterLink}`). */
function DrawerLink({ href, title, children }: { href: string; title?: string; children: ReactNode }) {
  return (
    <MuiLink component={LocalOverlayLink} href={href} scroll={false} aria-haspopup="dialog" title={title} underline="none" color="inherit" sx={{ display: "block" }}>
      {children}
    </MuiLink>
  );
}
