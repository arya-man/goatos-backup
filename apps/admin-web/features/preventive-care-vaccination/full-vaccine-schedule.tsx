import Link from "@/components/no-prefetch-link";
import { CalendarDays, Layers, MapPinned, Search, Warehouse, X } from "lucide-react";
import { type ApiResult } from "@/lib/api/server";
import { copy, optionGroup, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate, todayIso } from "@/lib/format";
import { backendScope, scopeHref, type Scope } from "@/lib/scope";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { ClipText, Tag, type Tone } from "@/components/ui-primitives";
import { getCalendarVaccinationEvents, type CalendarEvent, type CalendarEventListResponse } from "@/features/calendar";

const CURRENT_YEAR = Number(todayIso().slice(0, 4));
const CURRENT_MONTH = Number(todayIso().slice(5, 7));
const PAGE_LIMIT = 100;
const SHED_CHIP_PREVIEW_LIMIT = 2;
const VACCINE_CHIP_PREVIEW_LIMIT = 2;
const SHED_DRAWER_PAGE_SIZE = 12;

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

type ScheduleShedGroup = {
  shed: string;
  shedId?: string;
  count: number;
};

type DriveSummaryShedShape = {
  shed_id?: string | null;
  shedId?: string | null;
  shed_name?: string | null;
  shedName?: string | null;
  total_animals?: number | null;
  totalAnimals?: number | null;
};

function scheduleShedGroups(event: CalendarEvent): ScheduleShedGroup[] {
  return ((event.drive_summary?.sheds ?? []) as DriveSummaryShedShape[])
    .map((shed) => ({
      shed: String(shed.shed_name ?? shed.shedName ?? "").trim(),
      shedId: String(shed.shed_id ?? shed.shedId ?? "").trim() || undefined,
      count: Number(shed.total_animals ?? shed.totalAnimals ?? 0) || 0,
    }))
    .filter((shed) => shed.shed);
}

function ScheduleDrawerCloseForm({
  href,
  className,
  label,
  children,
}: {
  href: string;
  className: string;
  label: string;
  children?: React.ReactNode;
}) {
  const closeUrl = new URL(href, "http://mesha.local");
  return (
    <form action={closeUrl.pathname} method="get">
      {Array.from(closeUrl.searchParams.entries()).map(([key, value]) => (
        <input key={`${key}:${value}`} type="hidden" name={key} value={value} />
      ))}
      <button type="submit" className={className} aria-label={label}>
        {children}
      </button>
    </form>
  );
}

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

function selectedShedQuery(searchParams: RouteSearchParams | undefined): string {
  return (one(searchParams ?? {}, "schedule_sheds_q") ?? "").trim().slice(0, 80);
}

function selectedShedPage(searchParams: RouteSearchParams | undefined): number {
  return boundedInt(one(searchParams ?? {}, "schedule_sheds_page"), 1, 1, 1_000_000);
}

function monthLabel(year: number, month: number): string {
  return new Intl.DateTimeFormat("en", { month: "short", year: "numeric", timeZone: "Asia/Kolkata" }).format(new Date(Date.UTC(year, month - 1, 1)));
}

function monthWindowKeys(year: number, month: number): { from: string; to: string } {
  const from = `${year}-${String(month).padStart(2, "0")}-01`;
  const end = new Date(Date.UTC(year, month, 0));
  const to = `${year}-${String(month).padStart(2, "0")}-${String(end.getUTCDate()).padStart(2, "0")}`;
  return { from, to };
}

function eventDate(event: CalendarEvent): string {
  return (event.drive_summary?.due_date || event.due_at || "").slice(0, 10);
}

function eventDriveState(event: CalendarEvent, pageContract: AdminUiPageContract): ScheduleCellView {
  const summary = event.drive_summary;
  const date = eventDate(event);
  const label = date ? fmtDate(date) : copy(pageContract, "label.placeholder");
  if ((summary?.deferred_count ?? event.deferred_count ?? 0) > 0 || event.status === "deferred") {
    return { state: "due_soon", label, title: copy(pageContract, "schedule.state.deferred_title") };
  }
  if ((summary?.overdue_count ?? 0) > 0 || event.status === "overdue") {
    return { state: "overdue", label, title: copy(pageContract, "schedule.state.overdue_title") };
  }
  if ((summary?.due_count ?? 0) > 0 || event.status === "due") {
    return { state: "due_soon", label, title: copy(pageContract, "schedule.state.due_title") };
  }
  if (summary && summary.remaining_count === 0 && summary.total_count > 0) {
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

function scheduleLoadLines(event: CalendarEvent, animals: number, pageContract: AdminUiPageContract): string[] {
  const driveCount = event.drive_count ?? 0;
  const scheduled = event.scheduled_count ?? 0;
  const deferred = event.deferred_count ?? 0;
  const totalDoses = event.drive_summary?.total_count ?? event.target_count ?? animals;
  const lines = [
    `${animals} ${copy(pageContract, animals === 1 ? "schedule.unit.animal" : "schedule.unit.animals")}`,
  ];
  if (totalDoses !== animals) {
    lines.push(`${totalDoses} ${copy(pageContract, totalDoses === 1 ? "schedule.unit.dose" : "schedule.unit.doses")}`);
  }
  if (driveCount > 1) {
    lines.push(`${driveCount} ${copy(pageContract, "schedule.load.batches")}`);
  }
  if (deferred > 0) {
    lines.push(`${deferred} ${copy(pageContract, "schedule.load.deferred_short")}`);
  } else if (scheduled > 0) {
    lines.push(`${scheduled} ${copy(pageContract, "schedule.load.scheduled_short")}`);
  }
  return lines;
}

function dateEyebrow(date: string): string {
  if (!date) {
    return "";
  }
  return new Intl.DateTimeFormat("en", { weekday: "short", timeZone: "Asia/Kolkata" }).format(new Date(`${date}T00:00:00+05:30`));
}

async function loadFullSchedule(scope: Scope, year: number, month: number, cursor?: string): Promise<ApiResult<CalendarEventListResponse>> {
  const { parkId } = backendScope(scope);
  const { from, to } = monthWindowKeys(year, month);
  return getCalendarVaccinationEvents({
    parkId,
    dateFrom: from,
    dateTo: to,
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
  scheduleResult?: ApiResult<CalendarEventListResponse>;
}) {
  const year = selectedScheduleYear(searchParams);
  const month = selectedScheduleMonth(searchParams);
  const cursor = selectedScheduleCursor(searchParams);
  const selectedEventId = selectedScheduleEvent(searchParams);
  const shedQuery = selectedShedQuery(searchParams);
  const shedPage = selectedShedPage(searchParams);
  const result = scheduleResult ?? (await loadFullSchedule(scope, year, month, cursor));
  const rows = result.ok
    ? result.data.items
        .filter((event) => event.event_type === "vaccination_drive" || (event.drive_summary?.total_count ?? 0) > 0)
        .sort((a, b) =>
          [eventDate(a), a.drive_summary?.park_name ?? a.park_code ?? "", a.title, a.event_id]
            .join("|")
            .localeCompare([eventDate(b), b.drive_summary?.park_name ?? b.park_code ?? "", b.title, b.event_id].join("|")),
      )
    : [];
  const uniqueParks = new Set(rows.map((row) => row.park_id ?? row.drive_summary?.park_name ?? row.park_code).filter(Boolean));
  const totalSheds = rows.reduce((sum, row) => sum + (row.drive_summary?.shed_count ?? row.shed_count ?? 0), 0);
  const totalAnimals = rows.reduce((sum, row) => sum + (row.drive_summary?.total_animals ?? row.target_count ?? 0), 0);
  const overdueDrives = rows.filter((row) => eventDriveState(row, pageContract).state === "overdue").length;
  const scheduleTable = table(pageContract, "full-vaccine-schedule");
  const fixedColumns = scheduleTable.columns.filter((column) => column.visible);
  const legend = optionGroup(pageContract, "schedule_status_legend");
  const currentScheduleHref = scopeHref("/vaccination", scope, {}, { view: "schedule", schedule_year: String(year), schedule_month: String(month) });
  const selectedRow = selectedEventId ? rows.find((row) => row.event_id === selectedEventId) : undefined;
  const staleSchedule = Boolean(result.ok && (result.data.projection?.stale || result.data.projection?.partial_coverage));
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

  function shedDrawerHref(row: CalendarEvent, extra?: Record<string, string | undefined>): string {
    return scopeHref("/vaccination", scope, {}, {
      view: "schedule",
      schedule_year: String(year),
      schedule_month: String(month),
      schedule_event: row.event_id,
      schedule_sheds_page: "1",
      schedule_shed: undefined,
      goat_passport: undefined,
      ...extra,
    });
  }

  function drawerPageHref(page: number): string {
    if (!selectedRow) return currentScheduleHref;
    return shedDrawerHref(selectedRow, {
      schedule_sheds_q: shedQuery || undefined,
      schedule_sheds_page: String(page),
    });
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
                  <th>{fixedColumns.find((column) => column.key === "animals")?.label ?? copy(pageContract, "schedule.column.animals")}</th>
                  <th>{copy(pageContract, "schedule.column.vaccines")}</th>
                  <th>{copy(pageContract, "schedule.column.status")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const summary = row.drive_summary;
                  const view = eventDriveState(row, pageContract);
                  const vaccines = uniqueSorted([...(summary?.vaccine_labels ?? []), ...(row.vaccine_labels ?? [])]);
                  const sheds = uniqueSorted(row.shed_labels ?? []);
                  const shedCount = summary?.shed_count ?? row.shed_count ?? 0;
                  const animals = summary?.total_animals ?? row.target_count ?? 0;
                  const statusLabel = legend.find((item) => item.key === view.state)?.label ?? view.state;
                  const date = eventDate(row);
                  const shedTitle = sheds.join(", ") || row.shed_name || "";
                  const previewSheds = sheds.slice(0, SHED_CHIP_PREVIEW_LIMIT);
                  const hiddenShedCount = Math.max(0, shedCount - previewSheds.length);
                  const vaccineTitle = vaccines.join(", ");
                  const previewVaccines = vaccines.slice(0, VACCINE_CHIP_PREVIEW_LIMIT);
                  const hiddenVaccineCount = Math.max(0, vaccines.length - previewVaccines.length);
                  const loadLines = scheduleLoadLines(row, animals, pageContract);
                  return (
                    <tr key={row.event_id} className="schedule-click-row">
                      <td className={`schedule-date-cell state-${view.state}`} title={view.title}>
                        <a href={shedDrawerHref(row)} className="celllink schedule-date-link">
                          <span className="schedule-date-stack">
                            <span className="schedule-date-main">{view.label}</span>
                            <span className="schedule-date-sub">{dateEyebrow(date)}</span>
                          </span>
                        </a>
                      </td>
                      <td>
                        <a href={shedDrawerHref(row)} className="celllink">
                          <ClipText title={summary?.park_name ?? row.park_code ?? ""}>{summary?.park_name ?? row.park_code ?? copy(pageContract, "label.placeholder")}</ClipText>
                        </a>
                      </td>
                      <td>
                        <a href={shedDrawerHref(row)} className="celllink schedule-wrap-link schedule-shed-cell" title={shedTitle || copy(pageContract, "schedule.drawer.open_sheds")}>
                          <span className="schedule-chip-list schedule-chip-list-compact">
                            <span className="schedule-mini-chip schedule-count-chip">{countUnit(pageContract, shedCount, "schedule.unit.shed", "schedule.unit.sheds")}</span>
                            {previewSheds.map((shed) => (
                              <span key={shed} className="schedule-mini-chip">{shed}</span>
                            ))}
                            {hiddenShedCount > 0 ? (
                              <span className="schedule-mini-chip schedule-more-chip">+{hiddenShedCount} {copy(pageContract, "schedule.drawer.more")}</span>
                            ) : null}
                          </span>
                        </a>
                      </td>
                      <td className="muted num">
                        <a href={shedDrawerHref(row)} className="celllink num schedule-animals-link" title={loadLines.join(" · ")}>
                          <span className="schedule-load-stack">
                            <b>{animals}</b>
                            <span>{loadLines.slice(1).join(" · ") || copy(pageContract, "schedule.load.single_drive")}</span>
                          </span>
                        </a>
                      </td>
                      <td>
                        <a href={shedDrawerHref(row)} className="celllink schedule-wrap-link" title={vaccineTitle}>
                          <span className="schedule-chip-list vaccine-chip-list schedule-chip-list-compact">
                            {vaccines.length > 0 ? previewVaccines.map((vaccine) => (
                              <span key={vaccine} className="schedule-mini-chip vaccine-chip">{vaccine}</span>
                            )) : <span className="schedule-mini-chip">{copy(pageContract, "label.placeholder")}</span>}
                            {hiddenVaccineCount > 0 ? (
                              <span className="schedule-mini-chip schedule-more-chip">+{hiddenVaccineCount} {copy(pageContract, "schedule.drawer.more")}</span>
                            ) : null}
                          </span>
                        </a>
                      </td>
                      <td className={`schedule-status-cell state-${view.state}`} title={view.title}>
                        <a href={shedDrawerHref(row)} className="celllink schedule-status-link">
                          <Tag tone={STATE_TONE[view.state]}>{statusLabel}</Tag>
                        </a>
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
          {selectedRow ? (
            <ScheduleShedDrawer
              pageContract={pageContract}
              row={selectedRow}
              currentScheduleHref={currentScheduleHref}
              shedQuery={shedQuery}
              shedPage={shedPage}
              year={year}
              month={month}
              scope={scope}
              drawerPageHref={drawerPageHref}
            />
          ) : null}
        </>
      )}
    </section>
  );
}

async function ScheduleShedDrawer({
  pageContract,
  row,
  currentScheduleHref,
  shedQuery,
  shedPage,
  year,
  month,
  scope,
  drawerPageHref,
}: {
  pageContract: AdminUiPageContract;
  row: CalendarEvent;
  currentScheduleHref: string;
  shedQuery: string;
  shedPage: number;
  year: number;
  month: number;
  scope: Scope;
  drawerPageHref: (page: number) => string;
}) {
  const summaryShedGroups = scheduleShedGroups(row);
  const fallbackSheds = uniqueSorted(row.shed_labels ?? []).map((shed) => ({ shed, count: 0 }));
  const shedGroups = summaryShedGroups.length > 0 ? summaryShedGroups : fallbackSheds;
  const filteredShedGroups = shedQuery
    ? shedGroups.filter((group) => group.shed.toLowerCase().includes(shedQuery.toLowerCase()))
    : shedGroups;
  const totalPages = Math.max(1, Math.ceil(filteredShedGroups.length / SHED_DRAWER_PAGE_SIZE));
  const page = Math.min(shedPage, totalPages);
  const start = (page - 1) * SHED_DRAWER_PAGE_SIZE;
  const pageShedGroups = filteredShedGroups.slice(start, start + SHED_DRAWER_PAGE_SIZE);
  const summary = row.drive_summary;
  const date = eventDate(row);
  const vaccines = uniqueSorted([...(summary?.vaccine_labels ?? []), ...(row.vaccine_labels ?? [])]);
  const drawerBaseHref = scopeHref("/vaccination", scope, {}, {
      view: "schedule",
      schedule_year: String(year),
      schedule_month: String(month),
      schedule_event: row.event_id,
      schedule_sheds_q: shedQuery || undefined,
      schedule_sheds_page: String(page),
    });
  const selectedShedHref = (group: ScheduleShedGroup) => {
    if (group.shedId) {
      return scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(group.shedId)}`, scope, { mode: "park", park: row.park_id ?? scope.parkId }, {
        drive_due_date: (summary?.due_date ?? date) || undefined,
        ret: drawerBaseHref,
      }) + "#animals";
    }
    return scopeHref("/vaccination", scope, {}, {
      view: "schedule",
      schedule_year: String(year),
      schedule_month: String(month),
      schedule_event: row.event_id,
      schedule_sheds_q: shedQuery || undefined,
      schedule_sheds_page: String(page),
    });
  };

  return (
    <div className="schedule-drawer-backdrop" role="presentation">
      <ScheduleDrawerCloseForm href={currentScheduleHref} className="schedule-drawer-close-layer" label={copy(pageContract, "schedule.drawer.close")} />
      <aside className="schedule-side-drawer" role="dialog" aria-modal="false" aria-labelledby="schedule-shed-drawer-title">
        <div className="schedule-drawer-head">
          <div style={{ minWidth: 0 }}>
            <span className="eyebrow">{date ? `${fmtDate(date)} · ${summary?.park_name ?? row.park_code ?? copy(pageContract, "label.placeholder")}` : copy(pageContract, "label.placeholder")}</span>
            <h3 id="schedule-shed-drawer-title">{copy(pageContract, "schedule.drawer.title")}</h3>
            <p className="muted small">
              {shedGroups.length || row.shed_count || 0} {copy(pageContract, "schedule.unit.sheds")} · {summary?.total_animals ?? row.target_count ?? 0} {copy(pageContract, "schedule.unit.animals")} · {vaccines.join(", ") || copy(pageContract, "label.placeholder")}
            </p>
          </div>
          <ScheduleDrawerCloseForm href={currentScheduleHref} className="iconbtn" label={copy(pageContract, "schedule.drawer.close")}>
            <X className="ic" aria-hidden="true" />
          </ScheduleDrawerCloseForm>
        </div>

        <form className="schedule-drawer-search" action="/vaccination" method="get">
          <input type="hidden" name="view" value="schedule" />
          <input type="hidden" name="schedule_year" value={String(year)} />
          <input type="hidden" name="schedule_month" value={String(month)} />
          <input type="hidden" name="scope_mode" value={scope.mode} />
          {scope.parkId ? <input type="hidden" name="park" value={scope.parkId} /> : null}
          {scope.range !== "last_30_days" ? <input type="hidden" name="range" value={scope.range} /> : null}
          {scope.asOf ? <input type="hidden" name="as_of" value={scope.asOf} /> : null}
          {scope.domain ? <input type="hidden" name="domain" value={scope.domain} /> : null}
          {scope.range === "custom" && scope.dateFrom ? <input type="hidden" name="date_from" value={scope.dateFrom} /> : null}
          {scope.range === "custom" && scope.dateTo ? <input type="hidden" name="date_to" value={scope.dateTo} /> : null}
          <input type="hidden" name="schedule_event" value={row.event_id} />
          <input type="hidden" name="schedule_sheds_page" value="1" />
          <Search className="ic" aria-hidden="true" />
          <input name="schedule_sheds_q" defaultValue={shedQuery} placeholder={copy(pageContract, "schedule.drawer.search")} />
          <button className="btn sm" type="submit">{copy(pageContract, "schedule.drawer.search_action")}</button>
        </form>

        <div className="schedule-drawer-list" role="list" aria-label={copy(pageContract, "schedule.drawer.title")}>
          {pageShedGroups.length > 0 ? pageShedGroups.map((group) => (
            <a key={group.shed} href={selectedShedHref(group)} className="schedule-drawer-shed-row" role="listitem">
              <Warehouse className="ic" aria-hidden="true" />
              <span>{group.shed}</span>
              <Tag tone="info">{group.count > 0 ? `${group.count} ${copy(pageContract, "schedule.unit.animals")}` : copy(pageContract, "schedule.drawer.open_roster")}</Tag>
            </a>
          )) : (
            <div className="empty">{copy(pageContract, "schedule.drawer.empty")}</div>
          )}
        </div>

        <div className="schedule-drawer-foot">
          <span className="muted small">
            {copy(pageContract, "schedule.drawer.page_label")} {page} / {totalPages} · {filteredShedGroups.length} {copy(pageContract, "schedule.drawer.rows_label")}
          </span>
          <div className="chips">
            <a href={drawerPageHref(Math.max(1, page - 1))} className={`chip${page <= 1 ? " disabled" : ""}`} aria-disabled={page <= 1}>
              {copy(pageContract, "schedule.drawer.previous_page")}
            </a>
            <a href={drawerPageHref(Math.min(totalPages, page + 1))} className={`chip${page >= totalPages ? " disabled" : ""}`} aria-disabled={page >= totalPages}>
              {copy(pageContract, "schedule.drawer.next_page")}
            </a>
          </div>
        </div>
      </aside>
    </div>
  );
}
