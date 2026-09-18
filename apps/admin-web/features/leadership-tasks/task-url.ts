/**
 * The Tasks desk's URL vocabulary, as pure functions with NO imports.
 *
 * Alive on its own so `task-url.test.mjs` can exercise it under `node --test` without the `@/`
 * path alias, and because every rule in here is a rule the FRONTEND must enforce before a
 * request leaves: the list endpoint answers 400 for half a date range, an unknown sort or an
 * over-long `q` (CONTRACT.md "Validation rules"), and a filter bar that pushes those states puts
 * the reader on an error page for a keystroke.
 */

/** The list endpoint's closed sort enum. Anything else is a 400 `invalid_sort`. */
export const TASK_SORTS = ["raised_at_desc", "raised_at_asc", "deadline_asc", "deadline_desc"] as const;
export type TaskSort = (typeof TASK_SORTS)[number];
export const DEFAULT_TASK_SORT: TaskSort = "raised_at_desc";

export const TASK_SCOPES = ["assigned_to_me", "assigned_by_me", "team_progress"] as const;
export type TaskScope = (typeof TASK_SCOPES)[number];

export const TASK_FILTERS = ["all", "open", "in_progress", "done"] as const;
export type TaskFilter = (typeof TASK_FILTERS)[number];

/**
 * The two ways this desk renders the SAME list read: the status board and the table.
 *
 * `board` is the default because the board is what a reader compares against Jira, and because
 * the table is one click away and keeps every link it had. The view is a pure presentation
 * choice — it changes no request parameter, so switching it never re-queries and never
 * invalidates a cursor.
 */
export const TASK_VIEWS = ["board", "list"] as const;
export type TaskView = (typeof TASK_VIEWS)[number];
export const DEFAULT_TASK_VIEW: TaskView = "board";

export function normalizeTaskView(value: string | undefined): TaskView {
  return (TASK_VIEWS as readonly string[]).includes(value ?? "") ? (value as TaskView) : DEFAULT_TASK_VIEW;
}

/**
 * The board's columns, in the order a reader reads them: what has not started, what is moving,
 * what landed, what was dropped.
 *
 * `cancelled` is deliberately last AND deliberately outside `TASK_FILTERS`: the list endpoint
 * offers no `cancelled` filter and the response carries no whole-list count for it, so that
 * column can show rows but can never state a true total. The board says so rather than adding
 * up what happens to be on the page.
 */
export const TASK_BOARD_COLUMNS = ["open", "in_progress", "done", "cancelled"] as const;
export type TaskBoardColumn = (typeof TASK_BOARD_COLUMNS)[number];

/**
 * The columns a board renders under a status filter.
 *
 * ── WHY THE BOARD COLLAPSES INSTEAD OF IGNORING THE FILTER ────────────────────────────────────
 * A status filter and a status-column board say the SAME thing two ways, and when both were
 * active the board showed "0 on this page" under column pills reading 179 and 118: every number
 * correct on its own, the combination nonsense. Three behaviours were available —
 *   (a) the board ignores `filter` — but then the status chips are dead in board view and
 *       "see every task in this status" has nowhere to land;
 *   (b) the board collapses to the filtered column — one column, its TRUE whole-list total, and
 *       the whole keyset pager walking it;
 *   (c) the status segment is disabled in board view — which removes a control the reader can
 *       see working in list view, and makes a shared `?filter=done` link mean two different
 *       things depending on `t_view`.
 * (b) is the one where no combination of URL parameters can produce a board that looks empty
 * above counts that say otherwise: the columns the board draws are exactly the statuses the row
 * query is allowed to return. `all` draws all four; any other filter draws that one.
 */
export function boardColumnsForFilter(filter: TaskFilter): readonly TaskBoardColumn[] {
  if (filter === "all") return TASK_BOARD_COLUMNS;
  return TASK_BOARD_COLUMNS.filter((column) => column === filter);
}

/**
 * `view` is accepted as an ALIAS of `t_view` on read — and only on read.
 *
 * B5: `?view=list` was silently ignored, so a hand-written link looked like it selected a view
 * and did not. `view` cannot be this page's parameter NAME (three other screens own it, which is
 * what the `t_` prefix is for), but honouring it as a read-only alias costs nothing: the value is
 * still passed through `normalizeTaskView`, so `/vaccination?view=schedule` pasted onto `/tasks`
 * falls back to the default view rather than doing anything. `t_view` wins when both are present,
 * every link this page writes uses `t_view`, and the Board/List toggle DROPS a stray `view` so the
 * alias never survives past the first navigation.
 */
export const TASK_VIEW_ALIAS = "view";

export function resolveTaskView(prefixed: string | undefined, alias: string | undefined): TaskView {
  if (prefixed !== undefined && prefixed !== "") return normalizeTaskView(prefixed);
  return normalizeTaskView(alias);
}

/**
 * The UNPREFIXED names a reader plausibly types for a `t_`-prefixed parameter this page owns,
 * and which the page does NOT read. It IGNORES them LOUDLY: it names the parameter it did not
 * read, names the one it does, and offers the corrected link.
 *
 * `view` is not in this table because it IS read, as an alias (`resolveTaskView`). The rest stay
 * ignored rather than aliased because `q`, `sort`, `limit` and `page` are the generic names the
 * shell and several other worklists write, and honouring them here would filter this list with
 * another screen's state.
 */
export const UNPREFIXED_TASK_PARAM_ALIASES: Readonly<Record<string, string>> = {
  q: "t_q",
  sort: "t_sort",
  assignee: "t_assignee",
  raiser: "t_raiser",
  limit: "t_limit",
  page: "t_page",
  cursor: "t_cursor",
} as const;

/**
 * The aliases present in a URL whose real parameter is ABSENT, as `[alias, realName]` pairs.
 *
 * An alias beside its real parameter is not reported: `?view=list&t_view=board` is a link that
 * does select a view, and the prefixed one wins without ambiguity worth a banner.
 */
export function unprefixedTaskParamAliases(
  keys: readonly string[],
): Array<{ alias: string; param: string }> {
  const present = new Set(keys);
  return Object.entries(UNPREFIXED_TASK_PARAM_ALIASES)
    .filter(([alias, param]) => present.has(alias) && !present.has(param))
    .map(([alias, param]) => ({ alias, param }));
}

/** True when the whole-list count for this column is a number the backend actually publishes. */
export function boardColumnHasTotal(column: TaskBoardColumn): boolean {
  return (TASK_FILTERS as readonly string[]).includes(column);
}

/** `q` is trimmed and capped at 120 characters; over that is a 400 `invalid_query`. */
export const TASK_QUERY_MAX = 120;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isTaskUuid(value: string | undefined): boolean {
  return typeof value === "string" && UUID.test(value.trim());
}

/** A uuid filter, or undefined. A malformed one is DROPPED rather than sent: the backend answers
 *  400 `invalid_filter`, and a stale bookmark should degrade to the unfiltered list, not an error. */
export function taskUuidFilter(value: string | undefined): string | undefined {
  return isTaskUuid(value) ? value!.trim() : undefined;
}

export function normalizeTaskQuery(value: string | undefined): string | undefined {
  const trimmed = (value ?? "").trim();
  if (!trimmed) return undefined;
  return trimmed.slice(0, TASK_QUERY_MAX);
}

export function normalizeTaskSort(value: string | undefined): TaskSort {
  return (TASK_SORTS as readonly string[]).includes(value ?? "") ? (value as TaskSort) : DEFAULT_TASK_SORT;
}

export function normalizeTaskScope(value: string | undefined): TaskScope {
  return (TASK_SCOPES as readonly string[]).includes(value ?? "") ? (value as TaskScope) : "team_progress";
}

export function normalizeTaskFilter(value: string | undefined): TaskFilter {
  return (TASK_FILTERS as readonly string[]).includes(value ?? "") ? (value as TaskFilter) : "all";
}

const DATE_ONLY = /^\d{4}-\d{2}-\d{2}$/;
const DATE_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?(Z|[+-]\d{2}:\d{2})?$/;

export type TaskDateRange = {
  from?: string;
  to?: string;
  /**
   * The reader has typed HALF a range (or an inverted one) and the range is therefore NOT being
   * sent. The bar renders a note for this rather than letting the page 400 — both ends travel
   * together or neither does, the same rule `WorklistFilters`' `daterange` kind keeps.
   */
  incomplete: boolean;
};

/**
 * One inclusive span, sent only when BOTH ends are present, well-formed and in order.
 *
 * The raw ends are handed back so the inputs keep showing what the reader typed: silently
 * emptying the field they just filled is how a filter bar becomes unexplainable.
 */
export function normalizeTaskDateRange(from: string | undefined, to: string | undefined): TaskDateRange {
  const left = (from ?? "").trim();
  const right = (to ?? "").trim();
  const wellFormed = (value: string) => DATE_ONLY.test(value) || DATE_TIME.test(value);
  if (!left && !right) return { incomplete: false };
  if (!wellFormed(left) || !wellFormed(right)) {
    return { from: left || undefined, to: right || undefined, incomplete: true };
  }
  if (left > right) return { from: left, to: right, incomplete: true };
  return { from: left, to: right, incomplete: false };
}

export const TASKS_PATHNAME = "/tasks";
export const TASKS_PREVIEW_PATHNAME = "/tasks-preview";
const DEFAULT_RETURN_TO = `${TASKS_PATHNAME}?scope=assigned_by_me`;

/**
 * Where a write is allowed to send the browser afterwards.
 *
 * `startsWith("/tasks")` was the old test and it accepted `/tasks-preview` — the FIXTURE host —
 * as the landing page for a live write, so a real status change could return to a screen made of
 * fixture rows and look like it had done nothing. The path must be `/tasks` exactly, optionally
 * with a query; `/tasks-preview`, `/tasks/anything` and every off-site form fall back.
 */
export function safeTaskReturnTo(raw: string | undefined): string {
  const value = (raw ?? "").trim();
  if (!value.startsWith(TASKS_PATHNAME)) return DEFAULT_RETURN_TO;
  const rest = value.slice(TASKS_PATHNAME.length);
  if (rest !== "" && !rest.startsWith("?")) return DEFAULT_RETURN_TO;
  return value;
}
