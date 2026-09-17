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
