import { redirect } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import { actionFeedbackCopy, copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getWorkBoardSummary, listWorkBoardRows, type WorkBoardRow, type WorkBoardSummary } from "@/lib/api/work-board-server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { istDayPlus, todayIso } from "@/lib/format";
import { boundedInt, hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import type { WorkBoardLaneColumn } from "./work-board-board";
import { parseScope } from "@/lib/scope";
import { WorkBoardBoard } from "./work-board-board";
import { WorkBoardModal } from "./work-board-modal";
import {
  isWorkState,
  laneCursorParams,
  LANE_PARK_END,
  laneNextHref,
  laneParkCursorKey,
  laneParkResetParams,
  lanePreviousHref,
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

// A board read is bounded to ONE park per request (each source binds one park). "All parks" is
// therefore composed here: with no park chosen the board reads EVERY park the caller may see and
// merges each column, so the CEO opens on both parks (maintainer request 2026-09-12). The lane
// counts are summed from the same per-park summaries, so the header total always equals the work
// behind it, never a single park's share.
function mergeSummaries(summaries: WorkBoardSummary[]): WorkBoardSummary | null {
  if (summaries.length === 0) return null;
  const first = summaries[0]!;
  if (summaries.length === 1) return first;
  const addInto = (into: Record<string, number>, from: Record<string, number> | undefined) => {
    for (const [key, value] of Object.entries(from ?? {})) into[key] = (into[key] ?? 0) + value;
    return into;
  };
  const byLane: Record<string, number> = {};
  const byState: Record<string, number> = {};
  const byModule: Record<string, number> = {};
  let total = 0;
  let needsAttention = 0;
  const moduleSet = new Set<WorkBoardSummary["modules"][number]>();
  for (const s of summaries) {
    total += s.total;
    needsAttention += s.needs_attention;
    addInto(byLane, s.by_lane);
    addInto(byState, s.by_state);
    addInto(byModule, s.by_module);
    for (const m of s.modules) moduleSet.add(m);
  }
  return {
    ...first,
    total,
    needs_attention: needsAttention,
    by_lane: byLane,
    by_state: byState,
    by_module: byModule,
    modules: Array.from(moduleSet),
    park_id: "",
  };
}

// The Work Board: one business day, every module the caller may see, in four server-derived
// columns. The board's rows and the lane counts are two reads of the SAME filter, fired together;
// the counts are whole-filter, never the page's length.
export async function WorkBoardPage({ searchParams, pageContract }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const parks = parkOptions(pageContract);
  const chosenPark = scope.parkId ? parks.find((option) => option.key === scope.parkId) : undefined;
  // No park chosen = every park the caller has; a chosen chip narrows to that one park.
  const activeParks = chosenPark ? [chosenPark] : parks;
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
  // Reset clears every column's cursor for EVERY park (not just the active ones), so switching
  // between one park and all parks always starts each column at page one.
  const allParkKeys = parks.map((option) => option.key);
  const resetPaging = {
    [PARAM_CURSOR]: undefined,
    page: undefined,
    [`${PARAM_CURSOR}_stack`]: undefined,
    ...laneCursorParams(laneKeys),
    ...laneParkResetParams(laneKeys, allParkKeys),
  };

  if (activeParks.length === 0) {
    return (
      <section className="card">
        <div className="bd muted">{copy(pageContract, "state.empty")}</div>
      </section>
    );
  }

  const filterScope = { businessDate, modules: selectedModules, states: selectedStates, owner };
  // One read per (column, park), each on its own cursor: a bounded fan-out (lanes are a closed
  // set, parks are one or two), never per row. A park whose column is exhausted (its cursor is the
  // end sentinel) is not re-read.
  type LaneParkRead = { lane: string; parkKey: string; result: Awaited<ReturnType<typeof listWorkBoardRows>> };
  const laneParkPlan: { lane: string; parkKey: string; cursor?: string }[] = [];
  if (!noneSelected) {
    for (const lane of laneKeys) {
      for (const park of activeParks) {
        const raw = one(sp, laneParkCursorKey(lane, park.key));
        if (raw === LANE_PARK_END) continue;
        laneParkPlan.push({ lane, parkKey: park.key, cursor: raw && raw.length ? raw : undefined });
      }
    }
  }
  const summaryScopes = noneSelected ? [] : activeParks.map((park) => ({ ...filterScope, park: park.key }));
  const [summaryResults, laneReadResults] = await Promise.all([
    Promise.all(summaryScopes.map((scope) => getWorkBoardSummary(scope))),
    Promise.all(laneParkPlan.map((plan) => listWorkBoardRows({ ...filterScope, park: plan.parkKey, lane: plan.lane }, { limit, cursor: plan.cursor }))),
  ]);
  const laneParkReads: LaneParkRead[] = laneParkPlan.map((plan, i) => ({ lane: plan.lane, parkKey: plan.parkKey, result: laneReadResults[i]! }));
  // The wire's `modules` is the caller's set INTERSECTED with the request filter, so while a module
  // filter is on, the Module menu's vocabulary comes from one unfiltered summary read per park.
  const vocabularyResults =
    noneSelected || selectedModules.length ? await Promise.all(activeParks.map((park) => getWorkBoardSummary({ park: park.key, businessDate }))) : [];
  const allResults = [...summaryResults, ...vocabularyResults, ...laneReadResults];
  if (firstAuthRequiredError(...allResults)) redirect(INTERNAL_LOGIN_PATH);

  const rows: WorkBoardRow[] = laneReadResults.flatMap((result) => (result.ok ? result.data.rows : []));
  const summary = mergeSummaries(summaryResults.flatMap((result) => (result.ok ? [result.data] : [])));
  const okVocabulary = vocabularyResults.flatMap((result) => (result.ok ? [result.data] : []));
  const okSummary = summaryResults.flatMap((result) => (result.ok ? [result.data] : []));
  // Own-rows-only is a per-caller fact; any park saying so makes the board an own-rows board.
  const ownRowsOnly = [...okVocabulary, ...okSummary].some((data) => data.own_rows_only);
  const vocabularySummary = mergeSummaries(okVocabulary) ?? summary;
  const vocabulary = vocabularySummary ? vocabularySummary.modules : null;
  const visibleModules = vocabulary ? modulesVisible(allModules, vocabulary) : allModules;
  // Per-column pagers: each column reads all active parks and pages them TOGETHER on a shared page
  // number, with each park keeping its own keyset cursor `c_<lane>_<parkKey>`.
  const columns: WorkBoardLaneColumn[] = laneKeys.map((lane) => {
    const forLane = laneParkReads.filter((read) => read.lane === lane);
    const perPark = activeParks.map((park) => {
      const read = forLane.find((entry) => entry.parkKey === park.key);
      return { parkKey: park.key, nextCursor: read && read.result.ok ? read.result.data.next_cursor : undefined };
    });
    const laneRows = forLane.flatMap((read) => (read.result.ok ? read.result.data.rows : []));
    return {
      lane,
      rows: laneRows,
      nextHref: laneNextHref(WORK_BOARD_PATH, sp, lane, perPark),
      previousHref: lanePreviousHref(WORK_BOARD_PATH, sp, lane, allParkKeys),
      pageNumber: boundedInt(one(sp, `c_${lane}_page`), 1, 1, 1000000),
      // The footer's slice arithmetic spans the per-page capacity across active parks.
      pageSize: limit * activeParks.length,
    };
  });
  const selectedRow = one(sp, PARAM_ROW);
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const feedback = actionStatus && actionKey ? actionFeedbackCopy(pageContract, actionStatus, actionKey) : null;
  const closeHref = hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: undefined });
  const dateHref = (day: string) => hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_DATE]: day, ...resetPaging });
  const isToday = businessDate === todayIso();
  // Never blank the board for one slow module. Each successful read carries a `degraded` list
  // of the modules whose data could not load, and a read that failed outright is a module's
  // worth of outage too. The board blanks ONLY when EVERY read failed (a real outage);
  // otherwise it renders what loaded and names the rest to retry (maintainer decision 2026-09-12).
  const okResults = allResults.filter((result) => result.ok);
  const firstFail = allResults.find((result) => !result.ok);
  const error = okResults.length === 0 && firstFail && !firstFail.ok ? firstFail.error.message : null;
  const degradedKeys = new Set<string>();
  for (const result of okResults) {
    if (!result.ok) continue;
    const deg = (result.data as { degraded?: string[] }).degraded;
    if (Array.isArray(deg)) for (const key of deg) degradedKeys.add(key);
  }
  const someReadFailed = okResults.length > 0 && firstFail !== undefined;
  const degradedModules = allModules.filter((option) => degradedKeys.has(option.key));
  const partial = !error && (degradedModules.length > 0 || someReadFailed);
  const retryHref = hrefWithParams(WORK_BOARD_PATH, sp, {});

  // The "All parks" chip clears the park filter and returns to company scope; each park chip
  // narrows to that one park. Both reset every column to page one.
  const allParksHref = hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_PARK]: undefined, scope_mode: "company", [PARAM_OWNER]: undefined, ...resetPaging });
  const parkHrefs = Object.fromEntries(parks.map((option) => [option.key, hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_PARK]: option.key, scope_mode: "park", [PARAM_OWNER]: undefined, ...resetPaging })]));
  const hrefForRow = Object.fromEntries(rows.map((row) => [row.row_key, hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: row.row_key })]));
  // The role line names the SELECTION, as the mock does: every module the caller has, the chosen
  // few, or none at all.
  const chosenModules = noneSelected ? [] : selectedModules.length ? visibleModules.filter((option) => selectedModules.includes(option.key)) : visibleModules;
  const modulesLine = noneSelected ? copy(pageContract, "roleline.none") : chosenModules.length === visibleModules.length ? copy(pageContract, "roleline.all_modules") : chosenModules.map((option) => option.label).join(" + ");
  const parkLine = chosenPark ? chosenPark.label : copy(pageContract, "filter.park.all");
  const roleline = `${modulesLine} · ${parkLine}`;
  // A `row` in the URL that is not on THIS page (another park, another day, a later page, or a link
  // that was never valid) opens nothing; the board says so rather than ignoring it.
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
          selectedPark={chosenPark?.key ?? ""}
          parkHrefs={parkHrefs}
          allParksHref={allParksHref}
          businessDate={businessDate}
          isToday={isToday}
          previousDayHref={dateHref(istDayPlus(businessDate, -1))}
          nextDayHref={dateHref(istDayPlus(businessDate, 1))}
          hrefForRow={hrefForRow}
        />
      )}

      {partial ? (
        <div className="tag t-dng" role="status" style={{ display: "inline-block", marginTop: 10 }}>
          {copy(pageContract, "state.partial")}
          {degradedModules.length ? ` ${degradedModules.map((option) => option.label).join(", ")}.` : ""} <Link href={retryHref}>{copy(pageContract, "action.retry")}</Link>
        </div>
      ) : null}

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
