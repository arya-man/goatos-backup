import { redirect } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import { actionFeedbackCopy, copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getWorkBoardSummary, listWorkBoardRows, type WorkBoardRow } from "@/lib/api/work-board-server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { istDayPlus, todayIso } from "@/lib/format";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import type { WorkBoardLaneColumn } from "./work-board-board";
import { parseScope } from "@/lib/scope";
import { WorkBoardBoard } from "./work-board-board";
import { WorkBoardModal } from "./work-board-modal";
import {
  isWorkState,
  laneCursorParam,
  laneCursorParams,
  lanes,
  moduleOptions,
  modulesVisible,
  ownersOnPage,
  parkOptions,
  PARAM_CURSOR,
  PARAM_DATE,
  PARAM_LIMIT,
  PARAM_MODULE,
  PARAM_MODULE_NONE,
  PARAM_OWNER,
  PARAM_PARK,
  PARAM_ROW,
  PARAM_STATE,
  stateOptions,
  WORK_BOARD_PATH,
} from "./work-board-model";

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
  const requestedModules = csv(sp, PARAM_MODULE);
  // "Clear all" in the Module menu is an explicit EMPTY selection, carried in the URL as a
  // sentinel the API would refuse; the page reads nothing for it and renders empty columns,
  // exactly as the mock does. A missing or all-valid list means every module the caller has.
  const noneSelected = requestedModules.includes(PARAM_MODULE_NONE);
  const selectedModules = noneSelected ? [] : requestedModules.filter((key) => allModules.some((option) => option.key === key));
  const selectedStates = csv(sp, PARAM_STATE).filter((key) => isWorkState(key, allStates));
  const owner = one(sp, PARAM_OWNER);
  const pageSizes = tablePageSizes(pageContract, "work-board");
  const limit = pageSizes.find((size) => size === boundedInt(one(sp, PARAM_LIMIT), pageSizes[0] ?? 25, 1, 100)) ?? pageSizes[0] ?? 25;
  const laneKeys = lanes(pageContract).map((lane) => lane.key);
  const resetPaging = { [PARAM_CURSOR]: undefined, page: undefined, [`${PARAM_CURSOR}_stack`]: undefined, ...laneCursorParams(laneKeys) };

  if (!park) {
    return (
      <section className="card">
        <div className="bd muted">{copy(pageContract, "state.empty")}</div>
      </section>
    );
  }

  const boardScope = { park: park.key, businessDate, modules: selectedModules, states: selectedStates, owner };
  // One read PER COLUMN, each on its own cursor, plus the whole-filter summary: a fixed,
  // bounded fan-out of four column reads (the lanes are a closed set), never per row. A single
  // global page split 25 rows across four columns, so a column reading "117" showed three cards
  // and no way to the rest (maintainer report 2026-09-12).
  const laneReads = noneSelected
    ? laneKeys.map(() => null)
    : laneKeys.map((lane) => listWorkBoardRows({ ...boardScope, lane }, { limit, cursor: one(sp, laneCursorParam(lane)) }));
  const [summaryResult, ...laneResults] = await Promise.all([noneSelected ? null : getWorkBoardSummary(boardScope), ...laneReads]);
  const rowsResult = laneResults.find((result) => result !== null) ?? null;
  // The wire's `modules` is the caller's set INTERSECTED with the request filter, so while a
  // module filter is on, the Module menu's vocabulary comes from one unfiltered summary read.
  // It is a second, bounded round trip taken only in that case; with no filter, the filtered
  // summary IS that read. (The contract carrying the caller's vocabulary would remove it.)
  const vocabularyResult = noneSelected || selectedModules.length ? await getWorkBoardSummary({ park: park.key, businessDate }) : null;
  if (firstAuthRequiredError(...[summaryResult, vocabularyResult, ...laneResults].filter((result) => result !== null))) redirect(INTERNAL_LOGIN_PATH);

  const rows: WorkBoardRow[] = laneResults.flatMap((result) => (result?.ok ? result.data.rows : []));
  const summary = summaryResult?.ok ? summaryResult.data : null;
  const ownRowsOnly = (rowsResult?.ok ? rowsResult.data.own_rows_only : vocabularyResult?.ok ? vocabularyResult.data.own_rows_only : false) ?? false;
  const vocabulary = vocabularyResult ? (vocabularyResult.ok ? vocabularyResult.data.modules : null) : rowsResult?.ok ? rowsResult.data.modules : null;
  const visibleModules = vocabulary ? modulesVisible(allModules, vocabulary) : allModules;
  // Per-column pagers: each column's Next/Prev rides its own cursor key and page/stack twins.
  const columns: WorkBoardLaneColumn[] = laneKeys.map((lane, i) => {
    const result = laneResults[i];
    const key = laneCursorParam(lane);
    const nextCursor = result?.ok ? result.data.next_cursor : undefined;
    return {
      lane,
      rows: result?.ok ? result.data.rows : [],
      nextHref: hrefWithPagedCursor(WORK_BOARD_PATH, sp, key, nextCursor ?? null, `${key}_page`, `${key}_stack`),
      previousHref: hrefPreviousPagedCursor(WORK_BOARD_PATH, sp, key, `${key}_page`, `${key}_stack`),
      pageNumber: boundedInt(one(sp, `${key}_page`), 1, 1, 1000000),
      pageSize: limit,
    };
  });
  const selectedRow = one(sp, PARAM_ROW);
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const feedback = actionStatus && actionKey ? actionFeedbackCopy(pageContract, actionStatus, actionKey) : null;
  const closeHref = hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: undefined });
  const dateHref = (day: string) => hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_DATE]: day, ...resetPaging });
  const isToday = businessDate === todayIso();
  const failed = [summaryResult, vocabularyResult, ...laneResults].find((result) => result !== null && !result.ok);
  const error = failed && !failed.ok ? failed.error.message : null;

  const parkHrefs = Object.fromEntries(parks.map((option) => [option.key, hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_PARK]: option.key, scope_mode: "park", [PARAM_OWNER]: undefined, ...resetPaging })]));
  const hrefForRow = Object.fromEntries(rows.map((row) => [row.row_key, hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: row.row_key })]));
  // The role line names the SELECTION, as the mock does: every module the caller has, the
  // chosen few, or none at all.
  const chosenModules = noneSelected ? [] : selectedModules.length ? visibleModules.filter((option) => selectedModules.includes(option.key)) : visibleModules;
  const modulesLine = noneSelected ? copy(pageContract, "roleline.none") : chosenModules.length === visibleModules.length ? copy(pageContract, "roleline.all_modules") : chosenModules.map((option) => option.label).join(" + ");
  const roleline = `${modulesLine} · ${park.label}`;
  // A `row` in the URL that is not on THIS page (another park, another day, a later page, or a
  // link that was never valid) opens nothing; the board says so rather than ignoring it.
  const rowMissing = Boolean(selectedRow) && !error && !rows.some((row) => row.row_key === selectedRow);

  return (
    <div className="wb">
      <div className="crumb">{copy(pageContract, "crumb")}</div>
      <div className="phead">
        <h1>{copy(pageContract, "board.title")}</h1>
        <span className="muted small">{roleline}</span>
        <span className="sp" />
      </div>

      {feedback ? (
        <div className={`tag ${actionStatus === "success" ? "t-ok" : "t-dng"}`} role="status" style={{ display: "inline-block", marginBottom: 10 }}>
          {feedback}
        </div>
      ) : null}

      {error ? (
        <section className="card">
          <div className="bd" style={{ color: "var(--danger)" }}>{copy(pageContract, "state.error")}</div>
        </section>
      ) : (
        <WorkBoardBoard
          pageContract={pageContract}
          columns={columns}
          summary={summary}
          moduleOptions={visibleModules}
          selectedModules={selectedModules}
          noneSelected={noneSelected}
          owners={ownersOnPage(rows)}
          selectedOwner={owner}
          ownRowsOnly={ownRowsOnly}
          parks={parks}
          selectedPark={park.key}
          parkHrefs={parkHrefs}
          businessDate={businessDate}
          isToday={isToday}
          previousDayHref={dateHref(istDayPlus(businessDate, -1))}
          nextDayHref={dateHref(istDayPlus(businessDate, 1))}
          hrefForRow={hrefForRow}
        />
      )}

      {!error && !noneSelected && rows.length === 0 && (summary?.total ?? 0) === 0 ? (
        <div className="note muted small" style={{ marginTop: 8 }}>{ownRowsOnly ? copy(pageContract, "state.empty.own_rows") : copy(pageContract, "state.empty")}</div>
      ) : null}
      {rowMissing ? (
        <div className="note muted small" role="status" style={{ marginTop: 8 }}>
          {copy(pageContract, "state.row_missing")} <Link href={closeHref}>{copy(pageContract, "action.close")}</Link>
        </div>
      ) : null}

      <WorkBoardModal
        pageContract={pageContract}
        rows={rows}
        initialSelectedRowKey={selectedRow}
        closeHref={closeHref}
        returnToByRow={Object.fromEntries(rows.map((row) => [row.row_key, hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: row.row_key, action_status: undefined, action_key: undefined })]))}
      />
    </div>
  );
}
