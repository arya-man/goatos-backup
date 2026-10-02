import { listOrEmpty } from "@/lib/list-or-empty";
import { WB_HEADER_MB } from "./work-board-layout";
import { PageHeader } from "@/components/app/page-header";
import { redirect } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import { EmptyContent } from "@/components/minimal/empty-content";
import { actionFeedbackCopy, copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getWorkBoardPage, type WorkBoardLane, type WorkBoardRow, type WorkBoardSummary } from "@/lib/api/work-board-server";
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
  laneOffsetKey,
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
import { withLoadedDegradedCards, modulesWithWork } from "./work-board-counts";

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

function csv(sp: RouteSearchParams, key: string): string[] {
  const raw = sp[key];
  const parts = Array.isArray(raw) ? raw : raw ? [raw] : [];
  return parts
    .flatMap((part) => part.split(","))
    .map((part) => part.trim())
    .filter(Boolean);
}

function ownerOptionsFromVocabulary(vocabulary: Array<{ user_id?: string; name?: string }>): ReturnType<typeof ownersOnPage> | null {
  if (vocabulary.length === 0) return null;
  const seen = new Map<string, { id: string; name: string }>();
  for (const owner of vocabulary) {
    const id = owner.user_id?.trim();
    if (!id || seen.has(id)) continue;
    seen.set(id, { id, name: owner.name?.trim() || id.slice(0, 8) });
  }
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
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

type boundedRunner<T, R> = (item: T, index: number) => Promise<R>;

async function runBounded<T, R>(items: T[], limit: number, fn: boundedRunner<T, R>): Promise<R[]> {
  const out = new Array<R>(items.length);
  let next = 0;
  const workers = Math.max(1, Math.min(limit, items.length));
  await Promise.all(
    Array.from({ length: workers }, async () => {
      for (;;) {
        const index = next;
        next += 1;
        if (index >= items.length) return;
        // serial-await: allow bounded worker-pool await; parallelism is controlled by the caller's limit.
        out[index] = await fn(items[index]!, index);
      }
    }),
  );
  return out;
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
    return <EmptyContent filled title={copy(pageContract, "state.empty")} sx={{ py: 10 }} />;
  }

  const filterScope = { businessDate, modules: selectedModules, states: selectedStates, owner };
  // One backend read per park. The backend returns the summary plus every lane page while
  // preserving per-lane cursors, so opening "All parks" is at most two API requests instead of
  // two summaries plus eight row requests.
  type LaneParkRead = { lane: string; parkKey: string; result: { ok: true; data: { rows: WorkBoardRow[]; next_cursor?: string; degraded?: string[] } } | { ok: false; error: { message: string } } };
  const pagePlans: { parkKey: string; openLanes: WorkBoardLane[]; cursors: Partial<Record<WorkBoardLane, string | undefined>> }[] = [];
  for (const park of activeParks) {
    const cursors: Partial<Record<WorkBoardLane, string | undefined>> = {};
    const openLanes: WorkBoardLane[] = [];
    if (!noneSelected) {
      for (const lane of laneKeys) {
        const raw = one(sp, laneParkCursorKey(lane, park.key));
        if (raw === LANE_PARK_END) continue;
        const laneKey = lane as WorkBoardLane;
        cursors[laneKey] = raw && raw.length ? raw : undefined;
        openLanes.push(laneKey);
      }
    }
    pagePlans.push({ parkKey: park.key, openLanes, cursors });
  }
  // "Clear all" still reads each park's summary (no lanes: page_lane=__none__), because the Module
  // menu lists only the modules that have work and cannot know that from nothing.
  // A column's page is `limit` cards in TOTAL, not per park (pr294 L-A10): "All parks" read each
  // park at the full limit and stacked them, so To-do held 30 cards (~6,000px) beside short lanes.
  // Each park reads its share; per-park keyset cursors and the shared page number are unchanged.
  const perParkLimit = Math.max(1, Math.ceil(limit / activeParks.length));
  const pageResults = await runBounded(pagePlans, 1, (plan) => getWorkBoardPage({ ...filterScope, park: plan.parkKey }, { limit: perParkLimit, lanes: plan.openLanes, cursors: plan.cursors })); // request-plan:ignore owner=work-board issue=bounded-park-page-fanout expires=2027-03-31 reason=all-parks reads are serialized across parks; each backend page read serializes/short-circuits lane reads instead of SSR fanning out 10+ API calls
  const summaryResults = noneSelected ? [] : pageResults.map((result) => (result.ok ? { ok: true as const, data: result.data.summary } : result));
  const vocabularyResults = pageResults.flatMap((result) => (result.ok && (noneSelected || result.data.vocabulary_summary) ? [{ ok: true as const, data: result.data.vocabulary_summary ?? result.data.summary }] : []));
  const laneParkReads: LaneParkRead[] = [];
  if (!noneSelected) {
    pagePlans.forEach((plan, i) => {
      const result = pageResults[i]!;
      if (!result.ok) {
        for (const lane of plan.openLanes) laneParkReads.push({ lane, parkKey: plan.parkKey, result });
        return;
      }
      for (const lane of plan.openLanes) {
        laneParkReads.push({ lane, parkKey: plan.parkKey, result: { ok: true as const, data: result.data.lanes[lane as WorkBoardLane] ?? { rows: [] } } });
      }
    });
  }
  const allResults = [...summaryResults, ...vocabularyResults, ...pageResults];
  if (firstAuthRequiredError(...allResults)) redirect(INTERNAL_LOGIN_PATH);

  const rows: WorkBoardRow[] = laneParkReads.flatMap((read) => (read.result.ok ? listOrEmpty(read.result.data.rows) : []));
  const ownerOptions = ownerOptionsFromVocabulary(pageResults.flatMap((result) => (result.ok ? result.data.owner_vocabulary ?? [] : []))) || ownersOnPage(rows);
  // Which modules could not be COUNTED, per park: each page read carries its own deduped list.
  const degradedByPark = new Map<string, Set<string>>();
  pagePlans.forEach((plan, i) => {
    const result = pageResults[i];
    const deg = result && result.ok ? (result.data as { degraded?: string[] }).degraded : undefined;
    if (Array.isArray(deg) && deg.length) degradedByPark.set(plan.parkKey, new Set(deg));
  });
  const loadedCards = laneParkReads.flatMap((read) => (read.result.ok ? read.result.data.rows.map((row) => ({ parkKey: read.parkKey, lane: read.lane, module: row.module })) : []));
  const mergedSummary = mergeSummaries(summaryResults.flatMap((result) => (result.ok ? [result.data] : [])));
  // A module whose count timed out still loads its cards; they are added to the headings so a
  // column never reads fewer than the cards under it.
  const summary = mergedSummary ? withLoadedDegradedCards(mergedSummary, loadedCards, degradedByPark) : null;
  const okVocabulary = vocabularyResults.flatMap((result) => (result.ok ? [result.data] : []));
  const okSummary = summaryResults.flatMap((result) => (result.ok ? [result.data] : []));
  // Own-rows-only is a per-caller fact; any park saying so makes the board an own-rows board.
  const ownRowsOnly = [...okVocabulary, ...okSummary].some((data) => data.own_rows_only);
  const vocabularySummary = mergeSummaries(okVocabulary) ?? summary;
  const vocabulary = vocabularySummary ? vocabularySummary.modules : null;
  const degradedAnywhere = new Set<string>([...degradedByPark.values()].flatMap((set) => [...set]));
  // The menu offers only modules with work on the day (plus one that timed out or is selected).
  const visibleModules = vocabulary && vocabularySummary
    ? modulesWithWork(modulesVisible(allModules, vocabulary), vocabularySummary.by_module ?? {}, degradedAnywhere, selectedModules)
    : allModules;
  // Per-column pagers: each column reads all active parks and pages them TOGETHER on a shared page
  // number, with each park keeping its own keyset cursor `c_<lane>_<parkKey>`.
  const columns: WorkBoardLaneColumn[] = laneKeys.map((lane) => {
    const forLane = laneParkReads.filter((read) => read.lane === lane);
    const perPark = activeParks.map((park) => {
      const read = forLane.find((entry) => entry.parkKey === park.key);
      return { parkKey: park.key, nextCursor: read && read.result.ok ? read.result.data.next_cursor : undefined };
    });
    const laneRows = forLane.flatMap((read) => (read.result.ok ? listOrEmpty(read.result.data.rows) : []));
    return {
      lane,
      rows: laneRows,
      nextHref: laneNextHref(WORK_BOARD_PATH, sp, lane, perPark, laneRows.length),
      previousHref: lanePreviousHref(WORK_BOARD_PATH, sp, lane, allParkKeys),
      pageNumber: boundedInt(one(sp, `c_${lane}_page`), 1, 1, 1000000),
      offset: boundedInt(one(sp, laneOffsetKey(lane)), 0, 0, 1000000),
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

  const boardEmpty = !error && !noneSelected && rows.length === 0 && (summary?.total ?? 0) === 0;

  // Template sections/kanban/view/kanban-view.tsx: page heading, then the board track; an empty
  // board is the template EmptyContent (filled), status lines are MUI Alerts.
  return (
    <Box sx={{ minWidth: 0 }}>
      {/* Template CustomBreadcrumbs rhythm: mb { xs: 3, md: 5 } before the first block. */}
      <Box sx={{ mb: WB_HEADER_MB }}>
        <PageHeader title={copy(pageContract, "board.title")} crumbs={[{ label: copy(pageContract, "crumb") }, { label: roleline }]} />
      </Box>

      {feedback ? (
        <Alert severity={actionStatus === "success" ? "success" : "error"} role="status" sx={{ mb: 3 }}>
          {feedback}
        </Alert>
      ) : null}

      {error ? (
        <Alert severity="error" sx={{ mb: 3 }}>{copy(pageContract, "state.error")}</Alert>
      ) : (
        <WorkBoardBoard
          pageContract={pageContract}
          columns={columns}
          summary={summary}
          moduleOptions={visibleModules}
          selectedModules={selectedModules}
          noneSelected={noneSelected}
          owners={ownerOptions}
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
          empty={boardEmpty ? <EmptyContent filled title={ownRowsOnly ? copy(pageContract, "state.empty.own_rows") : copy(pageContract, "state.empty")} sx={{ py: 10 }} /> : null}
        />
      )}

      {partial ? (
        <Alert
          severity="warning"
          role="status"
          sx={{ mt: 3 }}
          action={
            <Button component={Link} href={retryHref} color="inherit" size="small">{copy(pageContract, "action.retry")}</Button>
          }
        >
          {copy(pageContract, "state.partial")}
          {degradedModules.length ? ` ${degradedModules.map((option) => option.label).join(", ")}.` : ""}
        </Alert>
      ) : null}

      {rowMissing ? (
        <Alert severity="info" role="status" sx={{ mt: 3 }} action={<Button component={Link} href={closeHref} color="inherit" size="small">{copy(pageContract, "action.close")}</Button>}>
          {copy(pageContract, "state.row_missing")}
        </Alert>
      ) : null}

      <WorkBoardModal
        pageContract={pageContract}
        rows={rows}
        initialSelectedRowKey={selectedRow}
        closeHref={closeHref}
        selectedOwner={owner}
        returnToByRow={Object.fromEntries(rows.map((row) => [row.row_key, hrefWithParams(WORK_BOARD_PATH, sp, { [PARAM_ROW]: row.row_key, action_status: undefined, action_key: undefined })]))}
      />
    </Box>
  );
}
