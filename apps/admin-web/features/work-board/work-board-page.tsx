import { redirect } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import { copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getWorkBoardSummary, listWorkBoardRows, type WorkBoardRow } from "@/lib/api/work-board-server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope } from "@/lib/scope";
import { WorkBoardDrawer } from "./work-board-drawer";
import { WorkBoardLanes } from "./work-board-lanes";
import {
  isWorkState,
  moduleOptions,
  modulesVisible,
  ownersOnPage,
  parkOptions,
  PARAM_CURSOR,
  PARAM_DATE,
  PARAM_LIMIT,
  PARAM_MODULE,
  PARAM_OWNER,
  PARAM_ROW,
  PARAM_STATE,
  stateOptions,
  WORK_BOARD_PATH,
} from "./work-board-model";
import { WorkBoardToolbar } from "./work-board-toolbar";

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

function csv(sp: RouteSearchParams, key: string): string[] {
  const raw = sp[key];
  const parts = Array.isArray(raw) ? raw : raw ? [raw] : [];
  return parts
    .flatMap((part) => part.split(","))
    .map((part) => part.trim())
    .filter(Boolean);
}

// The Work Board: one park, one business day, every module the caller may see, in four
// server-derived columns. The board's rows and the lane counts are two reads of the SAME
// filter, fired together; the counts are whole-filter, never the page's length.
export async function WorkBoardPage({ searchParams, pageContract }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const parks = parkOptions(pageContract);
  // The board is bounded to one park per request. The top-bar park chip carries the choice;
  // company-wide scope opens on the first park rather than fanning out one read per park.
  const park = parks.find((option) => option.key === scope.parkId) ?? parks[0];
  const requestedDate = one(sp, PARAM_DATE);
  const businessDate = requestedDate && DATE_RE.test(requestedDate) ? requestedDate : todayIso();
  const allModules = moduleOptions(pageContract);
  const allStates = stateOptions(pageContract);
  const selectedModules = csv(sp, PARAM_MODULE).filter((key) => allModules.some((option) => option.key === key));
  const selectedStates = csv(sp, PARAM_STATE).filter((key) => isWorkState(key, allStates));
  const owner = one(sp, PARAM_OWNER);
  const pageSizes = tablePageSizes(pageContract, "work-board");
  const limit = pageSizes.find((size) => size === boundedInt(one(sp, PARAM_LIMIT), pageSizes[0] ?? 25, 1, 100)) ?? pageSizes[0] ?? 25;
  const cursor = one(sp, PARAM_CURSOR);

  if (!park) {
    return (
      <section className="card">
        <div className="bd muted">{copy(pageContract, "state.empty")}</div>
      </section>
    );
  }

  const boardScope = { park: park.key, businessDate, modules: selectedModules, states: selectedStates, owner };
  const [rowsResult, summaryResult] = await Promise.all([listWorkBoardRows(boardScope, { limit, cursor }), getWorkBoardSummary(boardScope)]);
  if (firstAuthRequiredError(rowsResult, summaryResult)) redirect(INTERNAL_LOGIN_PATH);

  const rows: WorkBoardRow[] = rowsResult.ok ? rowsResult.data.rows : [];
  const summary = summaryResult.ok ? summaryResult.data : null;
  const ownRowsOnly = rowsResult.ok ? rowsResult.data.own_rows_only : false;
  const visibleModules = rowsResult.ok ? modulesVisible(allModules, rowsResult.data.modules) : allModules;
  const nextCursor = rowsResult.ok ? rowsResult.data.next_cursor : undefined;
  const nextHref = hrefWithPagedCursor(WORK_BOARD_PATH, sp, PARAM_CURSOR, nextCursor ?? null);
  const previousHref = hrefPreviousPagedCursor(WORK_BOARD_PATH, sp, PARAM_CURSOR);
  const selectedRow = one(sp, PARAM_ROW);
  const closeHref = hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: undefined });
  const dateHref = (day: string) => hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_DATE]: day, [PARAM_CURSOR]: undefined, page: undefined, [`${PARAM_CURSOR}_stack`]: undefined });
  const isToday = businessDate === todayIso();
  const error = !rowsResult.ok ? rowsResult.error.message : !summaryResult.ok ? summaryResult.error.message : null;

  return (
    <>
      <div className="phead" style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
        <h1 style={{ margin: 0 }}>{copy(pageContract, "page.title")}</h1>
        <span className="muted small">
          {visibleModules.length === allModules.length ? copy(pageContract, "filter.module.all") : visibleModules.map((option) => option.label).join(" + ")} · {park.label}
        </span>
        <span style={{ flex: 1 }} />
        <span className="datenav" style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
          <Link className="btn sm" href={dateHref(istDayPlus(businessDate, -1))} aria-label={copy(pageContract, "action.previous")}>
            ‹
          </Link>
          <span style={{ fontWeight: 700, padding: "0 6px" }}>
            {fmtDate(businessDate)}
            {isToday ? <span className="tag t-ok" style={{ marginLeft: 8 }}>{copy(pageContract, "filter.date.today")}</span> : null}
          </span>
          <Link className="btn sm" href={dateHref(istDayPlus(businessDate, 1))} aria-label={copy(pageContract, "action.next")}>
            ›
          </Link>
        </span>
      </div>

      <WorkBoardToolbar
        pageContract={pageContract}
        moduleOptions={visibleModules}
        stateOptions={allStates}
        selectedModules={selectedModules}
        selectedStates={selectedStates}
        owners={ownersOnPage(rows)}
        selectedOwner={owner}
        ownRowsOnly={ownRowsOnly}
      />

      <div className="muted small" style={{ display: "flex", gap: 14, flexWrap: "wrap", margin: "-4px 0 12px" }}>
        <span>{copy(pageContract, "board.rule")}</span>
        <span>{copy(pageContract, "board.attention")}</span>
      </div>

      {error ? (
        <section className="card">
          <div className="bd" style={{ color: "var(--danger)" }}>{copy(pageContract, "state.error")}</div>
        </section>
      ) : rows.length === 0 && (summary?.total ?? 0) === 0 ? (
        <section className="card">
          <div className="bd muted">{ownRowsOnly ? copy(pageContract, "state.empty.own_rows") : copy(pageContract, "state.empty")}</div>
        </section>
      ) : (
        <WorkBoardLanes pageContract={pageContract} rows={rows} summary={summary} drawerHrefForRow={(row) => hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: row.row_key })} />
      )}

      <div className="pager2" style={{ marginTop: 12 }}>
        <span className="small muted" style={{ marginRight: "auto" }}>
          {rows.length} {copy(pageContract, "pager.rows")}
          {summary ? ` · ${summary.total}` : ""}
        </span>
        {previousHref ? (
          <Link className="btn sm" href={previousHref}>{copy(pageContract, "action.previous")}</Link>
        ) : (
          <span className="btn sm" aria-disabled="true">{copy(pageContract, "action.previous")}</span>
        )}
        {nextHref ? (
          <Link className="btn sm" href={nextHref}>{copy(pageContract, "action.next")}</Link>
        ) : (
          <span className="btn sm" aria-disabled="true">{copy(pageContract, "action.next")}</span>
        )}
      </div>

      <WorkBoardDrawer pageContract={pageContract} rows={rows} initialSelectedRowKey={selectedRow} closeHref={closeHref} />
    </>
  );
}
