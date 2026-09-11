import { redirect } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import { actionFeedbackCopy, copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getWorkBoardSummary, listWorkBoardRows, type WorkBoardRow } from "@/lib/api/work-board-server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { istDayPlus, todayIso } from "@/lib/format";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope } from "@/lib/scope";
import { WorkBoardBoard } from "./work-board-board";
import { WorkBoardModal } from "./work-board-modal";
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
  const cursor = one(sp, PARAM_CURSOR);

  if (!park) {
    return (
      <section className="card">
        <div className="bd muted">{copy(pageContract, "state.empty")}</div>
      </section>
    );
  }

  const boardScope = { park: park.key, businessDate, modules: selectedModules, states: selectedStates, owner };
  const [rowsResult, summaryResult] = await Promise.all([
    noneSelected ? null : listWorkBoardRows(boardScope, { limit, cursor }),
    noneSelected ? null : getWorkBoardSummary(boardScope),
  ]);
  // The wire's `modules` is the caller's set INTERSECTED with the request filter, so while a
  // module filter is on, the Module menu's vocabulary comes from one unfiltered summary read.
  // It is a second, bounded round trip taken only in that case; with no filter, the filtered
  // summary IS that read. (The contract carrying the caller's vocabulary would remove it.)
  const vocabularyResult = noneSelected || selectedModules.length ? await getWorkBoardSummary({ park: park.key, businessDate }) : null;
  if (firstAuthRequiredError(...[rowsResult, summaryResult, vocabularyResult].filter((result) => result !== null))) redirect(INTERNAL_LOGIN_PATH);

  const rows: WorkBoardRow[] = rowsResult?.ok ? rowsResult.data.rows : [];
  const summary = summaryResult?.ok ? summaryResult.data : null;
  const ownRowsOnly = (rowsResult?.ok ? rowsResult.data.own_rows_only : vocabularyResult?.ok ? vocabularyResult.data.own_rows_only : false) ?? false;
  const vocabulary = vocabularyResult ? (vocabularyResult.ok ? vocabularyResult.data.modules : null) : rowsResult?.ok ? rowsResult.data.modules : null;
  const visibleModules = vocabulary ? modulesVisible(allModules, vocabulary) : allModules;
  const nextCursor = rowsResult?.ok ? rowsResult.data.next_cursor : undefined;
  const nextHref = hrefWithPagedCursor(WORK_BOARD_PATH, sp, PARAM_CURSOR, nextCursor ?? null);
  const previousHref = hrefPreviousPagedCursor(WORK_BOARD_PATH, sp, PARAM_CURSOR);
  const selectedRow = one(sp, PARAM_ROW);
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const feedback = actionStatus && actionKey ? actionFeedbackCopy(pageContract, actionStatus, actionKey) : null;
  const closeHref = hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: undefined });
  const dateHref = (day: string) => hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_DATE]: day, [PARAM_CURSOR]: undefined, page: undefined, [`${PARAM_CURSOR}_stack`]: undefined });
  const isToday = businessDate === todayIso();
  const failed = [rowsResult, summaryResult, vocabularyResult].find((result) => result !== null && !result.ok);
  const error = failed && !failed.ok ? failed.error.message : null;

  const parkHrefs = Object.fromEntries(parks.map((option) => [option.key, hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_PARK]: option.key, scope_mode: "park", [PARAM_OWNER]: undefined, [PARAM_CURSOR]: undefined, page: undefined, [`${PARAM_CURSOR}_stack`]: undefined })]));
  const hrefForRow = Object.fromEntries(rows.map((row) => [row.row_key, hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: row.row_key })]));
  // The role line names the SELECTION, as the mock does: every module the caller has, the
  // chosen few, or none at all.
  const chosenModules = noneSelected ? [] : selectedModules.length ? visibleModules.filter((option) => selectedModules.includes(option.key)) : visibleModules;
  const modulesLine = noneSelected ? copy(pageContract, "roleline.none") : chosenModules.length === visibleModules.length ? copy(pageContract, "roleline.all_modules") : chosenModules.map((option) => option.label).join(" + ");
  const roleline = `${modulesLine} · ${park.label}`;
  // The showing line counts from the page number the cursor helper tracks in the URL.
  const pageNumber = boundedInt(one(sp, "page"), 1, 1, 1000000);
  const first = rows.length ? (pageNumber - 1) * limit + 1 : 0;
  const last = rows.length ? (pageNumber - 1) * limit + rows.length : 0;

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
          rows={rows}
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

      {error ? null : (
      <div className="pager">
        <span>
          {copy(pageContract, "drawer.subtasks.showing")} <b>{first}–{last}</b> {copy(pageContract, "drawer.subtasks.of")} <b>{summary?.total ?? rows.length}</b> {copy(pageContract, "pager.rows")}
        </span>
        <span className="pgnav">
          {previousHref ? (
            <Link className="more" href={previousHref}>‹ {copy(pageContract, "action.previous")}</Link>
          ) : (
            <span className="more" aria-disabled="true">‹ {copy(pageContract, "action.previous")}</span>
          )}
          {nextHref ? (
            <Link className="more" href={nextHref}>{copy(pageContract, "action.next")} ›</Link>
          ) : (
            <span className="more" aria-disabled="true">{copy(pageContract, "action.next")} ›</span>
          )}
        </span>
      </div>
      )}

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
