import Link from "@/components/no-prefetch-link";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { RouteSearchParams } from "@/lib/search-params";
import type { LeadershipTaskAssignee, LeadershipTaskPage } from "@/lib/api/server";

import { changeLeadershipTaskStatusAction } from "./actions";
import { TASK_PARAM, tasksHref } from "./params";
import { TaskBoardColumns, type BoardColumnMeta } from "./task-board-dnd";
import { initials } from "./task-presentation";
import type { TaskRow } from "./task-row";
import {
  boardColumnHasTotal,
  TASK_BOARD_COLUMNS,
  TASKS_PATHNAME,
  type TaskBoardColumn,
  type TaskFilter,
} from "./task-url";

/** How many people the avatar group shows before it collapses into a "+N". */
const AVATAR_GROUP_MAX = 6;

/**
 * The status board: one column per status, cards stacked inside it.
 *
 * ── WHY THE COLUMNS CANNOT BE COMPLETE, AND WHAT THEY SAY INSTEAD ─────────────────────────────
 * The list endpoint is keyset-paginated, caps `limit` at 50, and publishes no offset and no
 * total row count. There is therefore no honest way to render "every open task" in a column, and
 * fetching status by status would be four requests that still could not fill four columns.
 *
 * So the board renders the page of the list the reader is already on, arranged by status, and is
 * explicit about it:
 *   - the number beside a column's name is `filters[].count` — the TRUE whole-list count for that
 *     status, which is the one number on this screen that is not a page artefact;
 *   - beneath it, the count of cards actually on this page, worded as such;
 *   - when the total exceeds what is shown, the column offers "show only this status", which sets
 *     `filter=<status>` and hands the whole keyset pager to that one column. That is the honest
 *     "show more": paging one status is something the endpoint can really do.
 *   - `cancelled` has no `filters[]` entry at all, so its count renders as the contract's
 *     placeholder with a title saying the total is not published. It is never summed from the
 *     rows on the page, because that number would be a lie about the list.
 *
 * DRAG AND DROP now lives in `task-board-dnd.tsx`, which owns the whole column track because a
 * drop moves a card between columns and that is client state. This component stays the SERVER
 * half: it reads the response, decides each column's wording, its honest totals and its
 * "show only this" link, mints the card deep links (the URL vocabulary lives server-side), and
 * hands all of it plus the status action down. It decides nothing about the drag — legality is
 * the row's own `status_options`, and the reasons a drop can be refused are documented on the
 * client component.
 */
export function LeadershipTasksBoard({
  pageContract,
  rows,
  filters,
  basePath,
  sp,
  scopeKey,
  activeFilter,
  selectedTaskID,
  assignees,
  assigneeUserID,
}: {
  pageContract: AdminUiPageContract;
  rows: TaskRow[];
  /** The response's whole-list per-status counts, verbatim. */
  filters: NonNullable<LeadershipTaskPage["filters"]>;
  basePath: string;
  sp: RouteSearchParams;
  scopeKey: string;
  activeFilter: TaskFilter;
  selectedTaskID?: string;
  assignees: LeadershipTaskAssignee[];
  assigneeUserID?: string;
}) {
  // Keyed by plain string: `cancelled` is a board column the response's filter enum does not
  // contain, and looking it up must be a miss, not a type error.
  const totals = new Map<string, (typeof filters)[number]>(
    filters.map((filter) => [filter.key, filter]),
  );
  const placeholder = copy(pageContract, "label.placeholder");

  const columns: BoardColumnMeta[] = TASK_BOARD_COLUMNS.map((column) => {
    const filter = totals.get(column);
    const cards = rows.filter((task) => task.status === column);
    const hasTotal = boardColumnHasTotal(column) && typeof filter?.count === "number";
    return {
      key: column,
      label: filter?.label ?? columnFallbackLabel(pageContract, column),
      total: hasTotal ? filter!.count : null,
      emptyMessage:
        filter?.empty_message ||
        copy(pageContract, "board.column_empty", "Nothing in this status on this page."),
      // Narrowing to a status is only offered when the endpoint HAS that filter and when the
      // whole list holds more of it than this page is showing.
      focusHref:
        boardColumnHasTotal(column) && hasTotal && filter!.count > cards.length
          ? tasksHref(
              basePath,
              sp,
              { [TASK_PARAM.filter]: column, [TASK_PARAM.task]: null },
              { resetPaging: true },
            )
          : null,
    };
  });

  /**
   * The card deep links, minted HERE because the URL vocabulary (`tasksHref`, `TASK_PARAM`) is
   * server-side. The drag layer is handed a finished map rather than the parameter grammar.
   */
  const cardHrefs: Record<string, string> = {};
  for (const task of rows) {
    cardHrefs[task.id] = tasksHref(basePath, sp, {
      [TASK_PARAM.scope]: scopeKey,
      [TASK_PARAM.task]: task.id,
    });
  }

  /**
   * Where a drop's redirect lands: THIS board, same scope, same page of the list, and NOT
   * selecting the dragged task — a drag is not a selection. `safeTaskReturnTo` in `actions.ts`
   * refuses anything that is not `/tasks` exactly, so the preview host round-trips to the real
   * desk rather than to its own fixture rows.
   */
  const returnTo = tasksHref(
    TASKS_PATHNAME,
    sp,
    { [TASK_PARAM.scope]: scopeKey },
    { resetPaging: false },
  );

  return (
    <div className="ltb">
      <PeopleFilter
        pageContract={pageContract}
        assignees={assignees}
        assigneeUserID={assigneeUserID}
        basePath={basePath}
        sp={sp}
      />

      <TaskBoardColumns
        pageContract={pageContract}
        columns={columns}
        rows={rows}
        cardHrefs={cardHrefs}
        selectedTaskID={selectedTaskID}
        activeFilter={activeFilter}
        placeholder={placeholder}
        action={changeLeadershipTaskStatusAction}
        returnTo={returnTo}
      />

      <p className="ltb-note">
        {copy(
          pageContract,
          "board.partial_note",
          "Each column shows the tasks on this page of the list. The number beside a status is its true total across the whole list; open one status to page through all of it.",
        )}
      </p>
    </div>
  );
}

/**
 * The overlapping avatar group: Jira's "filter by person" control, minted as ordinary links that
 * set `t_assignee`, so it needs no client JavaScript and cannot drift from the assignee dropdown
 * in the filter bar — both write the same parameter.
 */
function PeopleFilter({
  pageContract,
  assignees,
  assigneeUserID,
  basePath,
  sp,
}: {
  pageContract: AdminUiPageContract;
  assignees: LeadershipTaskAssignee[];
  assigneeUserID?: string;
  basePath: string;
  sp: RouteSearchParams;
}) {
  if (!assignees.length) return null;
  // The selected person is pinned into view: a filter whose own chip has scrolled out of the
  // group is a filter the reader cannot turn off.
  const selectedIndex = assignees.findIndex((person) => person.user_id === assigneeUserID);
  const shown =
    selectedIndex >= AVATAR_GROUP_MAX
      ? [assignees[selectedIndex]!, ...assignees.slice(0, AVATAR_GROUP_MAX - 1)]
      : assignees.slice(0, AVATAR_GROUP_MAX);
  const hidden = assignees.length - shown.length;
  const personHref = (userID: string | null) =>
    tasksHref(basePath, sp, { [TASK_PARAM.assignee]: userID }, { resetPaging: true });
  return (
    <div
      className="ltb-people"
      role="group"
      aria-label={copy(pageContract, "board.people_aria", "Filter by person")}
    >
      <Link
        href={personHref(null)}
        scroll={false}
        className={`ltb-person ltb-person-all${assigneeUserID ? "" : " on"}`}
        aria-current={assigneeUserID ? undefined : "true"}
      >
        {copy(pageContract, "board.people_all", "Everyone")}
      </Link>
      {shown.map((person) => {
        const name = person.name || person.title;
        const on = person.user_id === assigneeUserID;
        return (
          <Link
            key={person.user_id}
            href={personHref(on ? null : person.user_id)}
            scroll={false}
            className={`ltb-person${on ? " on" : ""}`}
            title={name}
            aria-label={`${copy(pageContract, "column.assignee")} ${name}`}
            aria-current={on ? "true" : undefined}
          >
            <span className="lt-avx" aria-hidden="true">
              {initials(name)}
            </span>
          </Link>
        );
      })}
      {hidden > 0 ? (
        <span className="ltb-person ltb-person-rest" aria-hidden="true">
          <span className="lt-avx">+{hidden}</span>
        </span>
      ) : null}
    </div>
  );
}

/**
 * The wording for a column the response does not describe. Only `cancelled` reaches this today:
 * the other three always arrive with a backend label, which is the wording the status chips use.
 *
 * NEVER THE COLUMN KEY. The third argument used to be `column` itself, so the branch that fires
 * when that assumption stops holding fired with the worst possible value and rendered the raw
 * wire token `in_progress` as a column heading. A status token is a wire value, not presentation
 * copy. Every column now has a contract key (`board.column.*` in the leadership-tasks page copy)
 * and a human sentence here for the deploy-skew window before the backend serves it.
 */
const COLUMN_FALLBACK_LABELS: Record<TaskBoardColumn, string> = {
  open: "Open",
  in_progress: "Doing",
  done: "Done",
  cancelled: "Cancelled",
};

function columnFallbackLabel(
  pageContract: AdminUiPageContract,
  column: TaskBoardColumn,
): string {
  return copy(
    pageContract,
    `board.column.${column}`,
    COLUMN_FALLBACK_LABELS[column],
  );
}
