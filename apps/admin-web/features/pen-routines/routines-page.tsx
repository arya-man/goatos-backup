import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { KpiRowSkeleton, TableSkeleton } from "@/components/app/skeletons";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Alert from "@mui/material/Alert";
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import { listOrEmpty } from "@/lib/list-or-empty";
import { ListChecks } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { PageHeader } from "@/components/app/page-header";
import { EmptyState } from "@/components/app/empty-state";
import Grid from "@mui/material/Grid";
import { CourseWidgetSummary } from "@/components/minimal/sections/overview/course/course-widget-summary";
import { COURSE_WIDGET_ICONS } from "@/lib/minimal-icons";
import { TableHeadCustom } from "@/components/minimal/table";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { LocalOverlayDrawer, type LocalOverlayDrawerItem } from "@/components/local-overlay-drawer";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { controlEnabled, copy, table, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  PenRoutineCatalog,
  PenRoutineListResponse,
  PenRoutinePark,
  PenRoutineRow,
  PenRoutineTaskListResponse,
  PenRoutineTaskRow,
} from "@/lib/api/pen-routines-server";
import type { ApiResult } from "@/lib/api/server";
import type { PaletteColorKey } from "@/theme/core";
import { fmtDate, todayIso } from "@/lib/format";
import { all, boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { RoutineDrawerForm, RoutineSaveFooter } from "./routine-drawer";
import { RoutinesDenseScope, RoutinesTableChrome, RoutinesToolbarRow } from "./routines-chrome";
import Button from "@mui/material/Button";

/**
 * /routines (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md): the routines of
 * a park -- the rule, its cadence and evidence lines, the roles it is for (with who holds them),
 * what it raised today -- and the
 * Today table of the tasks the kernel raised. Everything on screen is backend-owned: the copy
 * from the page contract, the rows and their composed lines (`cadence_line`, `evidence_line`,
 * `state_chip`, `operational_location_display`) from the reads, the park vocabulary from the list
 * response in the backend's own order (CBE, then CPT -- by code, never by name).
 *
 * Authoring is capability-gated on BOTH halves: the three controls on the contract (rendered only
 * when enabled; the backend's reason otherwise) and the route table behind each Server Action.
 *
 * The drawer is client-local: `?edit=new` / `?edit=<routine_id>` open it from data already on
 * the page through LocalOverlayLink, so opening and closing never re-run this Server Component.
 * The Today filter is a DIFFERENT param (`?routine=`) precisely so a reload with the drawer open
 * never reads as a task filter. The drawer's catalog (pens, roles, vocabularies) is read for ONE
 * park per request -- the page's park -- so a routine of the OTHER park opens through a real link
 * that selects its park first (`?park=<id>&edit=<id>`), and the create form switches park the
 * same way. One catalog read, never a fan-out over parks.
 */

export const ROUTINES_PATH = "/routines";
export const PARAM_PARK = "park";
export const PARAM_DAY = "day";
export const PARAM_ROUTINE = "routine";
export const PARAM_EDIT = "edit";
const PARAM_CURSOR = "cursor";
const PARAM_PAGE = "page";
const PARAM_STACK = "cursor_stack";
const PARAM_Q = "q";
const PARAM_ROUTINE_STATUS = "rstatus";
const PARAM_ROLE = "role";
const PARAM_STATE = "state";
const PARAM_ASSIGNEE = "assignee";
const PARAM_TO = "to";
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

const STATUS_COLOR: Record<PenRoutineRow["status"], LabelColor> = { active: "success", paused: "warning", retired: "default" };
const STATE_COLOR: Record<PenRoutineTaskRow["state_tone"], LabelColor> = { info: "info", review: "info", danger: "error", success: "success", muted: "default" };

/** Columns whose value is a count: right-aligned, tabular numerals (spec §3). */
const NUMERIC_ROUTINE_COLUMNS = new Set(["open_today", "delayed"]);

/** The current query string, so a client control can patch one parameter and keep the rest. */
function queryOf(params: RouteSearchParams): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (Array.isArray(value)) for (const item of value) next.append(key, item);
    else if (value) next.set(key, value);
  }
  return next.toString();
}

/** Distinct [value, label] pairs for a toolbar select, in first-seen order. */
function uniqueBy<T>(rows: T[], pick: (row: T) => [string, string]): { value: string; label: string }[] {
  const seen = new Map<string, string>();
  for (const row of rows) {
    const [value, label] = pick(row);
    if (value && !seen.has(value)) seen.set(value, label || value);
  }
  return Array.from(seen, ([value, label]) => ({ value, label }));
}

/** Read-only CSV of exactly the routines on screen. */
function routinesCsv(rows: PenRoutineRow[]): string {
  const head = ["name", "park", "cadence", "evidence", "status", "open_today", "delayed"];
  const body = rows.map((r) => [r.name, r.park_name, r.cadence_line, r.evidence_line, r.status_label, r.open_today, r.delayed]);
  return csvOf([head, ...body]);
}

/** Read-only CSV of exactly the Today tasks on screen. */
function tasksCsv(rows: PenRoutineTaskRow[]): string {
  const head = ["routine", "park", "location", "assignees", "state", "due"];
  const body = rows.map((r) => [r.routine_name, r.park_name, r.operational_location_display ?? "", r.assignee_names.join("; "), r.state_chip, r.due_business_date]);
  return csvOf([head, ...body]);
}

function csvOf(rows: Array<Array<string | number>>): string {
  return rows.map((r) => r.map((c) => `"${String(c).replace(/"/g, '""')}"`).join(",")).join("\r\n");
}

/** Rebuilds the page URL from the current params with a patch; paging keys are always dropped. */
function href(params: RouteSearchParams, patch: Record<string, string | undefined>, keepPaging = false): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (!keepPaging && (key === PARAM_CURSOR || key === PARAM_PAGE || key === PARAM_STACK)) continue;
    if (key in patch) continue;
    if (Array.isArray(value)) for (const item of value) next.append(key, item);
    else if (value) next.set(key, value);
  }
  for (const [key, value] of Object.entries(patch)) if (value) next.set(key, value);
  const qs = next.toString();
  return qs ? `${ROUTINES_PATH}?${qs}` : ROUTINES_PATH;
}

/** Next-page href: pushes the current cursor onto the stack the way lib/search-params' pager does. */
function nextHref(params: RouteSearchParams, cursor: string | null | undefined): string | null {
  if (!cursor) return null;
  const stack = all(params, PARAM_STACK);
  const current = one(params, PARAM_CURSOR);
  if (current) stack.push(current);
  const page = boundedInt(one(params, PARAM_PAGE), 1, 1, 1000000);
  const base = new URL(href(params, {}), "http://x");
  for (const item of stack.slice(-50)) base.searchParams.append(PARAM_STACK, item);
  base.searchParams.set(PARAM_CURSOR, cursor);
  base.searchParams.set(PARAM_PAGE, String(page + 1));
  return `${base.pathname}${base.search}`;
}

function previousHref(params: RouteSearchParams): string | null {
  const page = boundedInt(one(params, PARAM_PAGE), 1, 1, 1000000);
  if (page <= 1) return null;
  const stack = all(params, PARAM_STACK);
  const previous = stack.pop();
  const base = new URL(href(params, {}), "http://x");
  for (const item of stack) base.searchParams.append(PARAM_STACK, item);
  if (previous) {
    base.searchParams.set(PARAM_CURSOR, previous);
    if (page > 2) base.searchParams.set(PARAM_PAGE, String(page - 1));
  }
  return `${base.pathname}${base.search}`;
}

export type RoutinesPageData = {
  list: ApiResult<PenRoutineListResponse>;
  /** The drawer's vocabulary for `todayPark`; null when that read failed (the drawer then offers no pens or roles). */
  catalog: PenRoutineCatalog | null;
  tasks: ApiResult<PenRoutineTaskListResponse> | null;
  /** The park the Today table and the catalog were read for (the chosen park, else the first park served). */
  todayPark: PenRoutinePark | null;
  businessDate: string;
};

/** The parameters the page reads for its data, resolved once so page.tsx and the feature agree. */
export function routinesPageParams(sp: RouteSearchParams, pageContract: AdminUiPageContract) {
  const requestedDay = one(sp, PARAM_DAY);
  const businessDate = requestedDay && DATE_RE.test(requestedDay) ? requestedDay : todayIso();
  const pageSizes = tablePageSizes(pageContract, "pen-routine-tasks");
  const limit = pageSizes[0] ?? 25;
  return {
    parkId: one(sp, PARAM_PARK) || undefined,
    businessDate,
    routineId: one(sp, PARAM_ROUTINE) || undefined,
    cursor: one(sp, PARAM_CURSOR) || undefined,
    limit,
  };
}

export function RoutinesPage({ searchParams, pageContract, data }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract; data: RoutinesPageData }) {
  const sp = searchParams ?? {};
  const c = (key: string) => copy(pageContract, key);
  const canCreate = controlEnabled(pageContract, "create_routine", false);
  const canEdit = controlEnabled(pageContract, "edit_routine", false);
  const canSetStatus = controlEnabled(pageContract, "set_routine_status", false);
  const canConfigure = canCreate || canEdit || canSetStatus;

  const parks = data.list.ok ? listOrEmpty(data.list.data.parks) : [];
  const routines = data.list.ok ? listOrEmpty(data.list.data.rows) : [];
  const selectedPark = one(sp, PARAM_PARK) ?? "";
  const listHref = href(sp, { [PARAM_EDIT]: undefined }, true);
  const editHref = (id: string) => href(sp, { [PARAM_EDIT]: id }, true);


  const routinesTable = table(pageContract, "pen-routines");
  const tasksTable = table(pageContract, "pen-routine-tasks");
  const routineColumns = routinesTable.columns.filter((column) => column.visible);
  const taskColumns = tasksTable.columns.filter((column) => column.visible);

  const todayRoutines = data.todayPark ? routines.filter((routine) => routine.park_id === data.todayPark?.park_id) : routines;
  const routineFilterHrefs: Record<string, string> = { "": href(sp, { [PARAM_ROUTINE]: undefined }) };
  for (const routine of todayRoutines) routineFilterHrefs[routine.routine_id] = href(sp, { [PARAM_ROUTINE]: routine.routine_id });

  const tasks = data.tasks && data.tasks.ok ? data.tasks.data : null;
  const allTaskRows = tasks?.rows ?? [];
  const page = boundedInt(one(sp, PARAM_PAGE), 1, 1, 1000000);
  const currentQuery = queryOf(sp);
  const chromeLabels: Record<string, string> = {
    columns: copy(pageContract, "action.columns", "Columns"),
    export: copy(pageContract, "action.export", "Export"),
    more: copy(pageContract, "action.more", "More actions"),
    apply_search: copy(pageContract, "action.apply_search", "Apply search"),
    reset: copy(pageContract, "action.reset_filters", "Reset filters"),
    clear_all: copy(pageContract, "action.clear_all", "Clear all"),
    remove_filter: copy(pageContract, "action.remove_filter", "Remove filter"),
    dense: copy(pageContract, "action.dense", "Dense"),
    rows_per_page: copy(pageContract, "label.rows_per_page", "Rows per page:"),
    previous: c("action.previous"),
    next: c("action.next"),
  };

  // Toolbar filters. The backend reads take park / business date / routine / cursor only, so
  // search, status, role, state, assignee and the date range narrow the window already fetched.
  const routineQuery = (one(sp, PARAM_Q) ?? "").trim();
  const routineStatus = one(sp, PARAM_ROUTINE_STATUS) ?? "";
  const routineRole = one(sp, PARAM_ROLE) ?? "";
  const filteredRoutines = routines.filter((routine) => {
    if (routineQuery && !`${routine.name} ${routine.park_name}`.toLowerCase().includes(routineQuery.toLowerCase())) return false;
    if (routineStatus && routine.status !== routineStatus) return false;
    if (routineRole && !routine.assignee_roles.some((option) => option.key === routineRole)) return false;
    return true;
  });

  const taskQuery = (one(sp, PARAM_Q) ?? "").trim();
  const taskState = one(sp, PARAM_STATE) ?? "";
  const taskAssignee = one(sp, PARAM_ASSIGNEE) ?? "";
  const taskRows = allTaskRows.filter((row) => {
    if (taskQuery && !`${row.routine_name} ${row.park_name} ${row.operational_location_display ?? ""}`.toLowerCase().includes(taskQuery.toLowerCase())) return false;
    if (taskState && row.state_chip !== taskState) return false;
    if (taskAssignee && !row.assignee_names.includes(taskAssignee)) return false;
    return true;
  });

  const routineStatusOptions = uniqueBy(routines, (r) => [r.status, r.status_label]);
  const roleOptions = uniqueBy(
    routines.flatMap((r) => r.assignee_roles),
    (o) => [o.key, o.label],
  );
  const stateOptions = uniqueBy(allTaskRows, (r) => [r.state_chip, r.state_chip]);
  const assigneeOptions = uniqueBy(
    allTaskRows.flatMap((r) => r.assignee_names),
    (n) => [n, n],
  );
  const routineSizes = tablePageSizes(pageContract, "pen-routines");
  const taskSizes = tablePageSizes(pageContract, "pen-routine-tasks");

  const catalogParkId = data.todayPark?.park_id ?? "";
  // The create form's park switch: a real navigation that selects the park (and so its catalog)
  // and reopens the create drawer there.
  const createParkHrefs: Record<string, string> = {};
  for (const park of parks) createParkHrefs[park.park_id] = href(sp, { [PARAM_PARK]: park.park_id, [PARAM_ROUTINE]: undefined, [PARAM_EDIT]: "new" });
  const drawerItems: LocalOverlayDrawerItem[] = [];
  if (canCreate && catalogParkId) {
    drawerItems.push({
      id: "new",
      eyebrow: c("drawer.routine.title"),
      title: c("drawer.routine.create_title"),
      icon: <ListChecks className="ic" aria-hidden="true" />,
      body: <RoutineDrawerForm pageContract={pageContract} parks={parks} catalog={data.catalog} catalogParkId={catalogParkId} parkHrefs={createParkHrefs} canEdit={canCreate} canSetStatus={false} listHref={listHref} formId="prt-form-new" />,
      footer: <RoutineSaveFooter key="save" formId="prt-form-new" saveLabel={c("action.save")} canSave={canCreate} />,
    });
  }
  for (const routine of routines) {
    if (routine.park_id !== catalogParkId) continue;
    drawerItems.push({
      id: routine.routine_id,
      eyebrow: c("drawer.routine.title"),
      title: canEdit ? c("drawer.routine.edit_title") : routine.name,
      icon: <ListChecks className="ic" aria-hidden="true" />,
      body: (
        <RoutineDrawerForm
          pageContract={pageContract}
          routine={routine}
          parks={parks}
          catalog={data.catalog}
          catalogParkId={catalogParkId}
          parkHrefs={createParkHrefs}
          canEdit={canEdit}
          canSetStatus={canSetStatus}
          listHref={listHref}
          formId={`prt-form-${routine.routine_id}`}
        />
      ),
      footer: <RoutineSaveFooter key="save" formId={`prt-form-${routine.routine_id}`} saveLabel={c("action.save")} canSave={canEdit} />,
    });
  }
  // A routine of the page's park opens locally; one of the other park selects that park first.
  const openRoutine = (routine: PenRoutineRow, className: string | undefined, children: React.ReactNode) =>
    routine.park_id === catalogParkId ? (
      <LocalOverlayLink href={editHref(routine.routine_id)} scroll={false} className={className}>
        {children}
      </LocalOverlayLink>
    ) : (
      <Link href={href(sp, { [PARAM_PARK]: routine.park_id, [PARAM_ROUTINE]: undefined, [PARAM_EDIT]: routine.routine_id })} scroll={false} className={className}>
        {children}
      </Link>
    );

  const routineCell = (routine: PenRoutineRow, key: string) => {
    switch (key) {
      case "name":
        // Spec §3 identity cell: initial tile + primary line + the muted park line beneath.
        // Template user-table-row identity: Avatar + name over the muted secondary line.
        return openRoutine(
          routine,
          undefined,
          <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1.5, minWidth: 0, color: "inherit" }}>
            <Avatar variant="rounded" aria-hidden="true" sx={{ bgcolor: "primary.lighter", color: "primary.dark", typography: "caption", fontWeight: "fontWeightBold" }}>
              {initialsOf(routine.name)}
            </Avatar>
            <Box component="span" sx={{ display: "grid", minWidth: 0, typography: "body2" }}>
              <Box component="span" sx={{ fontWeight: "fontWeightSemiBold", color: "text.primary" }}>{routine.name}</Box>
              <Box component="span" sx={{ typography: "caption", color: "text.disabled", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{routine.park_name}</Box>
            </Box>
          </Box>,
        );
      case "park":
        return routine.park_name;
      case "cadence":
        return routine.cadence_line;
      case "evidence":
        return routine.evidence_line;
      case "people": {
        // The ONE person the routine is for (2026-09-26). A blank name means they no longer hold
        // a role for the park, so the routine raises nothing until someone else is chosen. A
        // routine written before then names its roles and who holds them.
        if (routine.assignee) {
          return routine.assignee.display_name ? (
            routine.assignee.display_name
          ) : (
            <Box component="span" sx={{ color: "warning.dark" }}>{copy(pageContract, "assignee.unavailable", c("label.placeholder"))}</Box>
          );
        }
        if (!routine.assignee_roles.length) return c("label.placeholder");
        const names = routine.people.map((person) => person.display_name).join(", ");
        return (
          <Box component="span" sx={{ display: "flex", flexDirection: "column", maxWidth: 260 }}>
            <span>{routine.assignee_roles.map((option) => option.label).join(", ")}</span>
            <Box component="span" title={names} sx={{ typography: "caption", color: "text.secondary", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
              {names ? `${c("table.people.preview")} ${names}` : c("empty.role_people")}
            </Box>
          </Box>
        );
      }
      case "status":
        return <Label variant="soft" color={STATUS_COLOR[routine.status]}>{routine.status_label}</Label>;
      case "open_today":
        return (
          <Box component="span" sx={{ display: "inline-flex", gap: 0.75, alignItems: "center", justifyContent: "flex-end" }}>
            <Box component="span" sx={{ fontWeight: "fontWeightBold", fontVariantNumeric: "tabular-nums" }}>{routine.open_today}</Box>
            {routine.delayed > 0 ? (
              <Label variant="soft" color="error">
                {routine.delayed} {c("summary.delayed")}
              </Label>
            ) : null}
          </Box>
        );
      default:
        return null;
    }
  };

  const taskCell = (row: PenRoutineTaskRow, key: string) => {
    switch (key) {
      case "routine":
        return row.routine_name;
      case "pen":
        // Backend-composed pen label, rendered verbatim: "Castro 2", "Godel 1 - Part 3". A whole-park
        // task names no pen, so the park it is for stands in its place.
        return row.scope_kind === "park" || !row.operational_location_display ? row.park_name : row.operational_location_display;
      case "assignee":
        return row.assignee_names.length ? row.assignee_names.join(", ") : c("label.placeholder");
      case "state_chip":
        return <Label variant="soft" color={STATE_COLOR[row.state_tone] ?? "default"}>{row.state_chip}</Label>;
      case "due":
        return fmtDate(row.due_business_date);
      default:
        return null;
    }
  };

  // The Today strip: the backend's own whole-filter aggregates, one KPI tile each, in the order
  // the reader works through them. Every number and word is the backend's.
  const summaryTiles: { key: keyof NonNullable<typeof tasks>["summary"]; copyKey: string; icon: string; color: PaletteColorKey }[] = [
    { key: "due", copyKey: "summary.due", icon: COURSE_WIDGET_ICONS.progress, color: "primary" },
    { key: "delayed", copyKey: "summary.delayed", icon: COURSE_WIDGET_ICONS.certificates, color: "error" },
    { key: "in_review", copyKey: "summary.in_review", icon: COURSE_WIDGET_ICONS.progress, color: "info" },
    { key: "sent_back", copyKey: "summary.sent_back", icon: COURSE_WIDGET_ICONS.certificates, color: "warning" },
    { key: "done", copyKey: "summary.done", icon: COURSE_WIDGET_ICONS.completed, color: "success" },
  ];

  return (
    <Box className="screen on routines-page">
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: c("crumb") }, { label: pageContract.title }]}
        actions={
          canCreate && catalogParkId ? (
            <Button component={LocalOverlayLink} href={editHref("new")} scroll={false} variant="contained" color="primary" startIcon={<Iconify icon="mingcute:add-line" />}>
              {c("action.create_routine.label")}
            </Button>
          ) : null
        }
      />

      <Stack spacing={3}>
        {!data.list.ok ? (
          <Alert severity="error">
            {data.list.error.message}
          </Alert>
        ) : null}

        {/* A deck of zeros is a wall, not a reading: the tiles render only once a day has counts. */}
        {/* KPI tiles and both tables' rows swap to their skeleton on a filter / park / day / page
            change (guard: url-keyed-panel); the card headers stay on screen. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<KpiRowSkeleton count={5} icon />}>
        {tasks && summaryTiles.some((tile) => Number(tasks.summary[tile.key]) > 0) ? (
          // Template overview/course: CourseWidgetSummary count tiles on a spacing-3 Grid.
          <Grid container spacing={3}>
            {summaryTiles.map((tile) => (
              <Grid key={tile.key} size={{ xs: 12, sm: 6, md: 4, lg: "grow" }}>
                <CourseWidgetSummary title={c(tile.copyKey)} total={tasks.summary[tile.key]} icon={tile.icon} color={tile.color} />
              </Grid>
            ))}
          </Grid>
        ) : null}
        </UrlSuspense>

        <Card className="kit-tablecard" aria-label={routinesTable.title}>
            <RoutinesDenseScope>
            <CardHeader
              sx={{ px: 3, pt: 2.5, pb: 1.5, alignItems: "center", gap: 1.5, flexWrap: "wrap" }}
              title={routinesTable.title}
              action={routines.length ? <Label variant="soft" color="info">{filteredRoutines.length}</Label> : null}
            />
            {!canConfigure ? <Alert severity="info" sx={{ mx: 3, mb: 1.5 }}>{c("configure.disabled_no_access")}</Alert> : null}
            {/* Filters stay OUTSIDE the keyed panel (guard: routines-toolbar-outside-panel). */}
            <RoutinesToolbarRow
              basePath={ROUTINES_PATH}
              currentQuery={currentQuery}
              searchParam={PARAM_Q}
              searchValue={routineQuery}
              searchPlaceholder={copy(pageContract, "filter.search_placeholder", c("filter.routine"))}
              searchLabel={copy(pageContract, "filter.search_label", c("filter.routine"))}
              filters={[
                {
                  param: PARAM_PARK,
                  label: c("filter.park"),
                  value: selectedPark,
                  allLabel: copy(pageContract, "filter.park.all", c("filter.park")),
                  options: parks.map((park) => ({ value: park.park_id, label: park.name })),
                },
                {
                  param: PARAM_ROUTINE_STATUS,
                  label: copy(pageContract, "filter.status", "Status"),
                  value: routineStatus,
                  allLabel: copy(pageContract, "filter.status.all", "All statuses"),
                  options: routineStatusOptions,
                },
                {
                  param: PARAM_ROLE,
                  label: copy(pageContract, "filter.role", "Role"),
                  value: routineRole,
                  allLabel: copy(pageContract, "filter.role.all", "All roles"),
                  options: roleOptions,
                },
              ]}
              clearable={[PARAM_Q, PARAM_PARK, PARAM_ROUTINE_STATUS, PARAM_ROLE]}
              cursorParams={[PARAM_CURSOR, PARAM_PAGE, PARAM_STACK]}
              shown={filteredRoutines.length}
              total={routines.length}
              csv={routinesCsv(filteredRoutines)}
              csvName="pen-routines.csv"
              labels={chromeLabels}
            />
            <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<TableSkeleton bare header={false} columns={routineColumns.length || 6} rows={8} />}>
            {routines.length === 0 ? (
              <EmptyState title={c("empty.routines")} sx={{ mx: 3, mb: 3 }} />
            ) : (
              <RoutinesTableChrome
                tableAriaLabel={routinesTable.title}
                basePath={ROUTINES_PATH}
                currentQuery={currentQuery}
                cursorParams={[PARAM_CURSOR, PARAM_PAGE, PARAM_STACK]}
                footer={{
                  nextHref: "",
                  hasPrevious: false,
                  pageSizeOptions: routineSizes,
                  pageSize: routineSizes[0] ?? 25,
                  limitParam: "rlimit",
                  rangeLabel: filteredRoutines.length === 0 ? "0" : `1–${filteredRoutines.length}`,
                }}
                labels={chromeLabels}
              >
                <Table stickyHeader>
                  <TableHeadCustom headCells={routineColumns.map((column) => ({ id: column.key, label: column.label, sortable: false, align: NUMERIC_ROUTINE_COLUMNS.has(column.key) ? "right" : undefined }))} />
                  <TableBody>
                    {filteredRoutines.map((routine) => (
                      <TableRow hover key={routine.routine_id}>
                        {routineColumns.map((column) => (
                          <TableCell key={column.key} align={NUMERIC_ROUTINE_COLUMNS.has(column.key) ? "right" : undefined} sx={NUMERIC_ROUTINE_COLUMNS.has(column.key) ? { fontVariantNumeric: "tabular-nums" } : undefined}>
                            {routineCell(routine, column.key)}
                          </TableCell>
                        ))}
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </RoutinesTableChrome>
            )}
            </UrlSuspense>
            </RoutinesDenseScope>
          </Card>

        <Card className="kit-tablecard" aria-label={tasksTable.title}>
            <RoutinesDenseScope>
            <CardHeader
              sx={{ px: 3, pt: 2.5, pb: 1.5, alignItems: "center", gap: 1.5, flexWrap: "wrap" }}
              title={tasksTable.title}
              subheader={data.todayPark?.name}
              action={data.todayPark ? <Label variant="soft" color="info">{taskRows.length}</Label> : null}
            />
            {data.tasks && !data.tasks.ok ? (
              <Alert severity="error" sx={{ mx: 3, mb: 2 }}>
                {data.tasks.error.message}
              </Alert>
            ) : null}
            {/* Filters stay OUTSIDE the keyed panel (guard: routines-toolbar-outside-panel). */}
            <RoutinesToolbarRow
              basePath={ROUTINES_PATH}
              currentQuery={currentQuery}
              searchParam={PARAM_Q}
              searchValue={taskQuery}
              searchPlaceholder={copy(pageContract, "filter.search_placeholder", c("filter.routine"))}
              searchLabel={copy(pageContract, "filter.search_label", c("filter.routine"))}
              filters={[
                {
                  param: PARAM_ROUTINE,
                  label: c("filter.routine"),
                  value: one(sp, PARAM_ROUTINE) ?? "",
                  allLabel: copy(pageContract, "filter.routine.all", c("filter.routine")),
                  options: todayRoutines.map((routine) => ({ value: routine.routine_id, label: routine.name })),
                },
                {
                  param: PARAM_STATE,
                  label: copy(pageContract, "filter.state", "State"),
                  value: taskState,
                  allLabel: copy(pageContract, "filter.state.all", "All states"),
                  options: stateOptions,
                },
                {
                  param: PARAM_ASSIGNEE,
                  label: copy(pageContract, "filter.assignee", "Assignee"),
                  value: taskAssignee,
                  allLabel: copy(pageContract, "filter.assignee.all", "All assignees"),
                  options: assigneeOptions,
                },
              ]}
              dateRange={{
                label: c("filter.business_date"),
                fromParam: PARAM_DAY,
                toParam: PARAM_TO,
                from: data.businessDate,
                to: one(sp, PARAM_TO) ?? "",
                fromLabel: c("filter.business_date"),
                toLabel: c("filter.business_date"),
              }}
              clearable={[PARAM_Q, PARAM_ROUTINE, PARAM_STATE, PARAM_ASSIGNEE, PARAM_TO]}
              cursorParams={[PARAM_CURSOR, PARAM_PAGE, PARAM_STACK]}
              shown={taskRows.length}
              total={allTaskRows.length}
              csv={tasksCsv(taskRows)}
              csvName="pen-routine-tasks.csv"
              labels={chromeLabels}
            />
            <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<TableSkeleton bare header={false} columns={taskColumns.length || 6} rows={10} />}>
            {allTaskRows.length === 0 ? (
              <EmptyState title={c("empty.tasks")} sx={{ mx: 3, mb: 3 }} />
            ) : (
              <RoutinesTableChrome
                tableAriaLabel={tasksTable.title}
                basePath={ROUTINES_PATH}
                currentQuery={currentQuery}
                cursorParams={[PARAM_CURSOR, PARAM_PAGE, PARAM_STACK]}
                footer={{
                  nextHref: nextHref(sp, tasks?.next_cursor) ?? "",
                  hasPrevious: Boolean(previousHref(sp)),
                  pageSizeOptions: taskSizes,
                  pageSize: taskSizes[0] ?? 25,
                  limitParam: "tlimit",
                  rangeLabel: taskRows.length === 0 ? "0" : `1–${taskRows.length} · ${c("label.page")} ${page}`,
                }}
                labels={chromeLabels}
              >
                <Table stickyHeader>
                  <TableHeadCustom headCells={taskColumns.map((column) => ({ id: column.key, label: column.label, sortable: false }))} />
                  <TableBody>
                    {taskRows.map((row) => (
                      <TableRow hover key={row.task_id}>
                        {taskColumns.map((column) => (
                          <TableCell key={column.key}>{taskCell(row, column.key)}</TableCell>
                        ))}
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </RoutinesTableChrome>
            )}
            </UrlSuspense>

            </RoutinesDenseScope>
          </Card>
      </Stack>

      <LocalOverlayDrawer items={drawerItems} selectionKey={PARAM_EDIT} initialSelectedId={one(sp, PARAM_EDIT)} closeHref={listHref} ariaLabel={c("drawer.routine.title")} closeLabel={c("action.close")} />
    </Box>
  );
}

/** Two letters for the routine identity tile; presentation only, it composes no copy. */
function initialsOf(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("");
}

/** Params that never change the tables: the routine drawer. */
const PANEL_IGNORE = [PARAM_EDIT] as const;
