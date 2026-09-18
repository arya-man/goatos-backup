import { all, boundedInt, one, type RouteSearchParams } from "@/lib/search-params";

import {
  DEFAULT_TASK_SORT,
  normalizeTaskDateRange,
  normalizeTaskFilter,
  normalizeTaskQuery,
  normalizeTaskScope,
  normalizeTaskSort,
  resolveTaskView,
  TASK_VIEW_ALIAS,
  taskUuidFilter,
  unprefixedTaskParamAliases,
  type TaskDateRange,
  type TaskFilter,
  type TaskScope,
  type TaskSort,
  type TaskView,
} from "./task-url";

/**
 * The Tasks desk's URL parameter NAMES.
 *
 * Every page-owned filter is `t_`-prefixed, and the two feedback params are `task_status` /
 * `task_code`. Both prefixes are collision avoidance, not decoration:
 *   - `features/vaccination-live-tracker/params.ts` already owns the bare `lt_*` space (its
 *     `lt_status` in particular), and the Tasks page shares the `.lt-page` stylesheet with it;
 *     two screens writing one param name is how a shared link starts filtering the wrong thing.
 *   - `q` / `sort` / `limit` unprefixed are the generic names the shell and several other
 *     worklists use.
 *
 * `scope` and `task` are deliberately NOT prefixed: `?scope=team_progress&task=<uuid>` is a deep
 * link that already exists in the wild (and in the shell's nav), and renaming it would break
 * every link anyone has saved.
 */
export const TASK_PARAM = {
  scope: "scope",
  task: "task",
  filter: "filter",
  q: "t_q",
  assignee: "t_assignee",
  raiser: "t_raiser",
  deadlineFrom: "t_deadline_from",
  deadlineTo: "t_deadline_to",
  raisedFrom: "t_raised_from",
  raisedTo: "t_raised_to",
  sort: "t_sort",
  view: "t_view",
  limit: "t_limit",
  cursor: "t_cursor",
  page: "t_page",
  cursorStack: "t_cursor_stack",
  feedbackStatus: "task_status",
  feedbackCode: "task_code",
  /**
   * The two FACTS a refusal carries beside its code, so the banner can say something a reader can
   * act on instead of "action could not be completed": the task's current backend `status_chip`,
   * and the name of the person whose move it is. Both are written by
   * `changeLeadershipTaskStatusAction` from a re-read of the task and rendered VERBATIM — the
   * banner substitutes them into a contract-owned sentence and composes no wording of its own.
   * They share the `task_` prefix of the two feedback params and are cleared with them.
   */
  feedbackStatusNow: "task_now",
  feedbackWho: "task_who",
} as const;

/** Every parameter the filter bar owns. Changing any of them RESTARTS paging. */
export const TASK_FILTER_PARAMS: readonly string[] = [
  TASK_PARAM.filter,
  TASK_PARAM.q,
  TASK_PARAM.assignee,
  TASK_PARAM.raiser,
  TASK_PARAM.deadlineFrom,
  TASK_PARAM.deadlineTo,
  TASK_PARAM.raisedFrom,
  TASK_PARAM.raisedTo,
  TASK_PARAM.sort,
];

/** Dropped whenever the filters change: a cursor minted over one result set is meaningless over
 *  another, and the backend rejects a cursor minted under a different sort outright. */
export const TASK_PAGING_PARAMS: readonly string[] = [
  TASK_PARAM.cursor,
  TASK_PARAM.page,
  TASK_PARAM.cursorStack,
];

export type TasksParams = {
  scope: TaskScope;
  filter: TaskFilter;
  q?: string;
  /** What the reader TYPED, kept so the input keeps showing it even when it was not sent. */
  rawQ: string;
  assigneeUserID?: string;
  raisedBy?: string;
  deadline: TaskDateRange;
  raised: TaskDateRange;
  sort: TaskSort;
  /**
   * Board or table. NOT in `TASK_FILTER_PARAMS`: it changes nothing about the request, so
   * switching views must keep the reader on the same page of the same list.
   */
  view: TaskView;
  limit: number;
  cursor?: string;
  page: number;
  /**
   * The repeated `t_cursor_stack` values, verbatim.
   *
   * Carried on the parsed state so a component that only receives `TasksParams` can rebuild the
   * WHOLE URL without being handed the raw search params — otherwise every link it mints
   * silently drops the reader's back-paging depth.
   */
  cursorStack: string[];
  selectedTaskID?: string;
  feedbackStatus?: string;
  feedbackCode?: string;
  feedbackStatusNow?: string;
  feedbackWho?: string;
  /**
   * The unprefixed parameter names in this URL that this page does NOT read, each beside the
   * `t_`-prefixed name it meant. Non-empty means a hand-written or pasted link is narrower (or
   * wider) than the reader thinks, and the page says so out loud rather than rendering a default
   * that looks like an answer. See `unprefixedTaskParamAliases`.
   */
  ignoredAliases: Array<{ alias: string; param: string }>;
};

/**
 * Reads the whole list state off the URL, dropping anything the list endpoint would answer 400
 * for. NOTHING about the top-bar scope (park / as_of / range / scope_mode / date_from / date_to)
 * is read here or anywhere in this feature: that scope is the shell's, and
 * `scripts/check-ia-guard.mjs` is explicit that a page must not re-read it.
 */
export function parseTasksParams(
  sp: RouteSearchParams | undefined,
  pageSizeOptions: readonly number[],
): TasksParams {
  const params = sp ?? {};
  const rawQ = one(params, TASK_PARAM.q) ?? "";
  const requestedLimit = boundedInt(one(params, TASK_PARAM.limit), defaultLimit(pageSizeOptions), 1, 50);
  const selectedTask = taskUuidFilter(one(params, TASK_PARAM.task));
  return {
    scope: normalizeTaskScope(one(params, TASK_PARAM.scope)),
    filter: normalizeTaskFilter(one(params, TASK_PARAM.filter)),
    q: normalizeTaskQuery(rawQ),
    rawQ,
    assigneeUserID: taskUuidFilter(one(params, TASK_PARAM.assignee)),
    raisedBy: taskUuidFilter(one(params, TASK_PARAM.raiser)),
    deadline: normalizeTaskDateRange(
      one(params, TASK_PARAM.deadlineFrom),
      one(params, TASK_PARAM.deadlineTo),
    ),
    raised: normalizeTaskDateRange(
      one(params, TASK_PARAM.raisedFrom),
      one(params, TASK_PARAM.raisedTo),
    ),
    sort: normalizeTaskSort(one(params, TASK_PARAM.sort)),
    // `t_view` wins; a bare `view` is honoured as a read-only alias (B5, see `resolveTaskView`).
    view: resolveTaskView(one(params, TASK_PARAM.view), one(params, TASK_VIEW_ALIAS)),
    // A page size the contract does not offer is not honoured: the backend caps at 50 and the
    // pager can only highlight a size it renders.
    limit: pageSizeOptions.includes(requestedLimit) ? requestedLimit : defaultLimit(pageSizeOptions),
    cursor: one(params, TASK_PARAM.cursor) || undefined,
    cursorStack: all(params, TASK_PARAM.cursorStack),
    page: boundedInt(one(params, TASK_PARAM.page), 1, 1, 1_000_000),
    selectedTaskID: selectedTask,
    feedbackStatus: one(params, TASK_PARAM.feedbackStatus),
    feedbackCode: one(params, TASK_PARAM.feedbackCode),
    feedbackStatusNow: one(params, TASK_PARAM.feedbackStatusNow),
    feedbackWho: one(params, TASK_PARAM.feedbackWho),
    ignoredAliases: unprefixedTaskParamAliases(Object.keys(params)),
  };
}

function defaultLimit(pageSizeOptions: readonly number[]): number {
  // 25 when the contract offers it (PageSizeOptions [5,10,25,50]); otherwise the largest size
  // below 25, so a contract that drops 25 still lands on a sensible page rather than 5.
  if (pageSizeOptions.includes(25)) return 25;
  const under = pageSizeOptions.filter((size) => size <= 25);
  return under.length ? Math.max(...under) : (pageSizeOptions[0] ?? 25);
}

/** True when the reader has narrowed the list with anything the bar owns (status aside). */
/**
 * Whether the request NARROWS the list: search text, a person, a date span or a status chip.
 * Unlike `hasTaskFilters` it ignores the sort, which reorders but hides nothing -- this is the
 * question the empty state and the card-header count ask (Gate-1 #2, #4): "are there tasks
 * this screen is not showing because of something the reader chose?"
 */
export function hasTaskNarrowing(params: TasksParams): boolean {
  return Boolean(
    params.q ||
      params.rawQ ||
      params.assigneeUserID ||
      params.raisedBy ||
      params.deadline.from ||
      params.deadline.to ||
      params.raised.from ||
      params.raised.to ||
      params.filter !== "all",
  );
}

export function hasTaskFilters(params: TasksParams): boolean {
  return Boolean(
    params.q ||
      params.rawQ ||
      params.assigneeUserID ||
      params.raisedBy ||
      params.deadline.from ||
      params.deadline.to ||
      params.raised.from ||
      params.raised.to ||
      params.sort !== DEFAULT_TASK_SORT,
  );
}

/**
 * A link that keeps the current URL and overrides some of it.
 *
 * Hand-rolled over `hrefWithParams` because every filter change must ALSO drop the three paging
 * params, and because the caller needs the repeated `t_cursor_stack` values preserved verbatim
 * when it is only changing, say, the selected task.
 */
export function tasksHref(
  pathname: string,
  sp: RouteSearchParams | undefined,
  overrides: Record<string, string | null | undefined>,
  options: { resetPaging?: boolean } = {},
): string {
  const params = sp ?? {};
  const next = new URLSearchParams();
  const dropped = new Set(Object.keys(overrides));
  if (options.resetPaging) for (const key of TASK_PAGING_PARAMS) dropped.add(key);
  for (const [key, value] of Object.entries(params)) {
    if (dropped.has(key)) continue;
    for (const item of Array.isArray(value) ? value : value ? [value] : []) {
      if (item) next.append(key, item);
    }
  }
  for (const [key, value] of Object.entries(overrides)) {
    if (value) next.set(key, value);
  }
  // Feedback is a one-shot: any navigation away from the banner clears it, so a bookmark or a
  // back press cannot resurrect "task updated" over a page that did nothing.
  next.delete(TASK_PARAM.feedbackStatus);
  next.delete(TASK_PARAM.feedbackCode);
  next.delete(TASK_PARAM.feedbackStatusNow);
  next.delete(TASK_PARAM.feedbackWho);
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

/** The URL with every bar-owned filter cleared, scope and selected task kept. */
export function tasksClearedHref(pathname: string, sp: RouteSearchParams | undefined): string {
  const overrides: Record<string, null> = {};
  for (const key of TASK_FILTER_PARAMS) overrides[key] = null;
  return tasksHref(pathname, sp, overrides, { resetPaging: true });
}

/**
 * The same URL with every ignored alias RENAMED to the parameter this page really reads.
 *
 * The one-click repair behind the ignored-parameter notice, so a reader handed a bad link does
 * not have to learn the parameter grammar to use it. Paging is reset because the aliases carry
 * filter and sort names, and a cursor minted over one result set means nothing over another.
 */
export function tasksAliasFixedHref(
  pathname: string,
  sp: RouteSearchParams | undefined,
  aliases: ReadonlyArray<{ alias: string; param: string }>,
): string {
  const params = sp ?? {};
  const overrides: Record<string, string | null> = {};
  for (const { alias, param } of aliases) {
    overrides[alias] = null;
    overrides[param] = one(params, alias) ?? null;
  }
  return tasksHref(pathname, params, overrides, { resetPaging: true });
}

/** The repeated cursor-stack values, exposed for the pager's own bookkeeping. */
export function taskCursorStack(sp: RouteSearchParams | undefined): string[] {
  return all(sp ?? {}, TASK_PARAM.cursorStack);
}

/**
 * The parsed state rendered BACK into search params.
 *
 * The seam for a component that is handed `TasksParams` and nothing else (the detail panel) but
 * still has to mint links that keep the whole URL — including the repeated cursor stack, which is
 * why `cursorStack` is parsed at all. Round-tripping the parsed state rather than the raw params
 * also means a value the parser rejected (a malformed uuid, an unknown sort) is not carried
 * forward into a link, which is the behaviour every other link on this page already has.
 */
export function tasksSearchParams(params: TasksParams): RouteSearchParams {
  const sp: Record<string, string | string[]> = {
    [TASK_PARAM.scope]: params.scope,
    [TASK_PARAM.filter]: params.filter,
    [TASK_PARAM.sort]: params.sort,
    [TASK_PARAM.view]: params.view,
    [TASK_PARAM.limit]: String(params.limit),
    [TASK_PARAM.page]: String(params.page),
  };
  const put = (key: string, value: string | undefined) => {
    if (value) sp[key] = value;
  };
  put(TASK_PARAM.q, params.rawQ);
  put(TASK_PARAM.assignee, params.assigneeUserID);
  put(TASK_PARAM.raiser, params.raisedBy);
  put(TASK_PARAM.deadlineFrom, params.deadline.from);
  put(TASK_PARAM.deadlineTo, params.deadline.to);
  put(TASK_PARAM.raisedFrom, params.raised.from);
  put(TASK_PARAM.raisedTo, params.raised.to);
  put(TASK_PARAM.cursor, params.cursor);
  put(TASK_PARAM.task, params.selectedTaskID);
  if (params.cursorStack.length) sp[TASK_PARAM.cursorStack] = params.cursorStack;
  return sp;
}
