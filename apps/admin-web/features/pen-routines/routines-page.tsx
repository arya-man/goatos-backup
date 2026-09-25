import { ListChecks } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayDrawer, type LocalOverlayDrawerItem } from "@/components/local-overlay-drawer";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { SegmentedLinks, type SegmentedOption } from "@/components/segmented-links";
import { Tag, type Tone } from "@/components/ui-primitives";
import { ProcurementPager } from "@/features/procurement";
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
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
import { all, boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { RoutineDrawerForm, RoutineSaveFooter } from "./routine-drawer";
import { RoutineFilter } from "./routine-filter";

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
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

const STATUS_TONE: Record<PenRoutineRow["status"], Tone> = { active: "ok", paused: "warn", retired: "mut" };
const STATE_TONE: Record<PenRoutineTaskRow["state_tone"], Tone> = { info: "info", review: "info", danger: "dng", success: "ok", muted: "mut" };

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

  const parks = data.list.ok ? data.list.data.parks : [];
  const routines = data.list.ok ? data.list.data.rows : [];
  const selectedPark = one(sp, PARAM_PARK) ?? "";
  const listHref = href(sp, { [PARAM_EDIT]: undefined }, true);
  const editHref = (id: string) => href(sp, { [PARAM_EDIT]: id }, true);
  const today = todayIso();
  const isToday = data.businessDate === today;

  // Park segments in the backend's served order: CBE, then CPT. The unfiltered segment carries
  // the filter's own label because the contract names no "all parks" sentence yet.
  const parkSegments: SegmentedOption[] = [
    { value: "", label: copy(pageContract, "filter.park.all", c("filter.park")), href: href(sp, { [PARAM_PARK]: undefined, [PARAM_ROUTINE]: undefined }) },
    ...parks.map((park) => ({ value: park.park_id, label: park.name, href: href(sp, { [PARAM_PARK]: park.park_id, [PARAM_ROUTINE]: undefined }) })),
  ];

  const routinesTable = table(pageContract, "pen-routines");
  const tasksTable = table(pageContract, "pen-routine-tasks");
  const routineColumns = routinesTable.columns.filter((column) => column.visible);
  const taskColumns = tasksTable.columns.filter((column) => column.visible);

  const todayRoutines = data.todayPark ? routines.filter((routine) => routine.park_id === data.todayPark?.park_id) : routines;
  const routineFilterHrefs: Record<string, string> = { "": href(sp, { [PARAM_ROUTINE]: undefined }) };
  for (const routine of todayRoutines) routineFilterHrefs[routine.routine_id] = href(sp, { [PARAM_ROUTINE]: routine.routine_id });

  const tasks = data.tasks && data.tasks.ok ? data.tasks.data : null;
  const taskRows = tasks?.rows ?? [];
  const page = boundedInt(one(sp, PARAM_PAGE), 1, 1, 1000000);

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
        return openRoutine(routine, undefined, routine.name);
      case "park":
        return routine.park_name;
      case "cadence":
        return routine.cadence_line;
      case "evidence":
        return routine.evidence_line;
      case "people": {
        // The roles the routine is for, then -- muted -- who holds them for its park right now.
        if (!routine.assignee_roles.length) return c("label.placeholder");
        const names = routine.people.map((person) => person.display_name).join(", ");
        return (
          <span style={{ display: "flex", flexDirection: "column", maxWidth: 260 }}>
            <span>{routine.assignee_roles.map((option) => option.label).join(", ")}</span>
            <span className="muted small" title={names} style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
              {names ? `${c("table.people.preview")} ${names}` : c("empty.role_people")}
            </span>
          </span>
        );
      }
      case "status":
        return <Tag tone={STATUS_TONE[routine.status]}>{routine.status_label}</Tag>;
      case "open_today":
        return (
          <span style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
            <span>{routine.open_today}</span>
            {routine.delayed > 0 ? (
              <Tag tone="dng">
                {routine.delayed} {c("summary.delayed")}
              </Tag>
            ) : null}
          </span>
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
        return <Tag tone={STATE_TONE[row.state_tone] ?? "mut"}>{row.state_chip}</Tag>;
      case "due":
        return fmtDate(row.due_business_date);
      default:
        return null;
    }
  };

  const summaryTiles: { key: keyof NonNullable<typeof tasks>["summary"]; copyKey: string; color?: string }[] = [
    { key: "due", copyKey: "summary.due" },
    { key: "delayed", copyKey: "summary.delayed", color: "var(--danger)" },
    { key: "in_review", copyKey: "summary.in_review" },
    { key: "sent_back", copyKey: "summary.sent_back", color: "var(--warn)" },
    { key: "done", copyKey: "summary.done", color: "var(--ok)" },
  ];

  return (
    <div className="screen on">
      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div>
          <div className="crumb">
            <b>{c("crumb")}</b>
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {parks.length > 1 ? <SegmentedLinks options={parkSegments} current={selectedPark} ariaLabel={c("filter.park")} /> : null}
      </div>

      {!data.list.ok ? (
        <div className="alert" role="alert">
          {data.list.error.message}
        </div>
      ) : null}

      <section className="card" aria-label={routinesTable.title}>
        <div className="hd">
          <ListChecks className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{routinesTable.title}</h3>
          <div className="sp" style={{ flex: 1 }} />
          {canCreate && catalogParkId ? (
            <LocalOverlayLink href={editHref("new")} scroll={false} className="btn sm primary">
              {c("action.create_routine.label")}
            </LocalOverlayLink>
          ) : null}
        </div>
        {!canConfigure ? <div className="note" style={{ margin: "10px 16px 0" }}>{c("configure.disabled_no_access")}</div> : null}
        <div className="bd">
          {routines.length === 0 ? (
            <div className="empty">{c("empty.routines")}</div>
          ) : (
            <div className="tablewrap" tabIndex={0} role="group" aria-label={routinesTable.title}>
              <table className="tbl">
                <thead>
                  <tr>
                    {routineColumns.map((column) => (
                      <th key={column.key}>{column.label}</th>
                    ))}
                    {canEdit ? <th aria-label={c("action.edit_routine.label")} /> : null}
                  </tr>
                </thead>
                <tbody>
                  {routines.map((routine) => (
                    <tr key={routine.routine_id}>
                      {routineColumns.map((column) => (
                        <td key={column.key}>{routineCell(routine, column.key)}</td>
                      ))}
                      {canEdit ? (
                        <td style={{ textAlign: "right" }}>{openRoutine(routine, "btn sm", c("action.edit_routine.label"))}</td>
                      ) : null}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </section>

      <section className="card" aria-label={tasksTable.title}>
        <div className="hd">
          <h3>
            {tasksTable.title}
            {data.todayPark ? <span className="muted"> · {data.todayPark.name}</span> : null}
          </h3>
          <div className="sp" style={{ flex: 1 }} />
          <div role="group" aria-label={c("filter.business_date")} style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            <Link href={href(sp, { [PARAM_DAY]: istDayPlus(data.businessDate, -1) })} scroll={false} className="btn sm" aria-label={c("action.previous")}>
              ‹
            </Link>
            <span className="small" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
              <span>{fmtDate(data.businessDate)}</span>
              {isToday ? <span className="pill">{c("table.tasks.title")}</span> : null}
            </span>
            <Link href={href(sp, { [PARAM_DAY]: istDayPlus(data.businessDate, 1) })} scroll={false} className="btn sm" aria-label={c("action.next")}>
              ›
            </Link>
          </div>
          {todayRoutines.length > 0 ? (
            <RoutineFilter
              label={c("filter.routine")}
              current={one(sp, PARAM_ROUTINE) ?? ""}
              options={todayRoutines.map((routine) => ({ value: routine.routine_id, label: routine.name }))}
              hrefFor={routineFilterHrefs}
            />
          ) : null}
        </div>
        <div className="bd">
          {data.tasks && !data.tasks.ok ? (
            <div className="alert" role="alert">
              {data.tasks.error.message}
            </div>
          ) : null}
          {tasks ? (
            <div className="grid g5 kpi-row prt-kpis" style={{ margin: "10px 0" }}>
              {summaryTiles.map((tile) => (
                <div className="kpi" key={tile.key}>
                  <div className="lab">{c(tile.copyKey)}</div>
                  <div className="val" style={tile.color && tasks.summary[tile.key] > 0 ? { color: tile.color } : undefined}>
                    {tasks.summary[tile.key]}
                  </div>
                </div>
              ))}
            </div>
          ) : null}
          {taskRows.length === 0 ? (
            <div className="empty">{c("empty.tasks")}</div>
          ) : (
            <div className="tablewrap" tabIndex={0} role="group" aria-label={tasksTable.title}>
              <table className="tbl">
                <thead>
                  <tr>
                    {taskColumns.map((column) => (
                      <th key={column.key}>{column.label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {taskRows.map((row) => (
                    <tr key={row.task_id}>
                      {taskColumns.map((column) => (
                        <td key={column.key}>{taskCell(row, column.key)}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <ProcurementPager prevHref={previousHref(sp)} nextHref={nextHref(sp, tasks?.next_cursor)} page={page} count={taskRows.length} noun="check" />
        </div>
      </section>

      <LocalOverlayDrawer items={drawerItems} selectionKey={PARAM_EDIT} initialSelectedId={one(sp, PARAM_EDIT)} closeHref={listHref} ariaLabel={c("drawer.routine.title")} closeLabel={c("action.close")} />
    </div>
  );
}
