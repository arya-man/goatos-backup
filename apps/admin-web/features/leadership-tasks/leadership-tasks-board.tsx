import Link from "@/components/no-prefetch-link";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { RouteSearchParams } from "@/lib/search-params";
import type { LeadershipTaskAssignee, LeadershipTaskPage } from "@/lib/api/server";

import { initials } from "./leadership-tasks-table";
import { TASK_PARAM, tasksHref } from "./params";
import { TaskBoardCard } from "./task-board-card";
import type { TaskRow } from "./task-row";
import {
  boardColumnHasTotal,
  TASK_BOARD_COLUMNS,
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
 * Drag and drop is deliberately NOT here. A status change goes through a server action fenced on
 * `row_version` with an idempotency key; a card dragged into a column that then 409s would move
 * back under the reader's hand with no explanation, which is worse than a board that does not
 * drag. Status changes stay in the detail panel this pass.
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

  const columns = TASK_BOARD_COLUMNS.map((column) => {
    const filter = totals.get(column);
    const cards = rows.filter((task) => task.status === column);
    const hasTotal = boardColumnHasTotal(column) && typeof filter?.count === "number";
    return {
      key: column,
      label: filter?.label ?? columnFallbackLabel(pageContract, column),
      total: hasTotal ? filter!.count : null,
      cards,
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

  return (
    <div className="ltb">
      <PeopleFilter
        pageContract={pageContract}
        assignees={assignees}
        assigneeUserID={assigneeUserID}
        basePath={basePath}
        sp={sp}
      />

      <div
        className="ltb-scroll"
        role="group"
        aria-label={copy(pageContract, "board.aria", "Tasks by status")}
      >
        <div className="ltb-cols">
          {columns.map((column) => (
            <section
              key={column.key}
              className={`ltb-col ltb-col-${column.key}${activeFilter === column.key ? " is-focused" : ""}`}
            >
              <header className="ltb-colhd">
                <span className="ltb-colname">{column.label}</span>
                <span
                  className="ltb-colcount"
                  title={
                    column.total === null
                      ? copy(
                          pageContract,
                          "board.total_unavailable",
                          "The whole-list total for this status is not published.",
                        )
                      : undefined
                  }
                >
                  {column.total === null ? placeholder : column.total}
                </span>
              </header>
              <div className="ltb-colmeta">
                <span>
                  {column.cards.length}{" "}
                  {copy(pageContract, "board.on_this_page", "on this page")}
                </span>
                {column.focusHref ? (
                  <Link href={column.focusHref} scroll={false} className="ltb-colmore">
                    {copy(pageContract, "board.focus_status", "Show only this")}
                  </Link>
                ) : null}
              </div>
              <div className="ltb-colbd">
                {column.cards.length ? (
                  column.cards.map((task) => (
                    <TaskBoardCard
                      key={task.id}
                      task={task}
                      pageContract={pageContract}
                      href={tasksHref(basePath, sp, {
                        [TASK_PARAM.scope]: scopeKey,
                        [TASK_PARAM.task]: task.id,
                      })}
                      selected={task.id === selectedTaskID}
                    />
                  ))
                ) : (
                  <p className="ltb-colempty">{column.emptyMessage}</p>
                )}
              </div>
            </section>
          ))}
        </div>
      </div>

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
 * The wording for a column the response does not describe. Only `cancelled` reaches this: the
 * other three always arrive with a backend label, which is the wording the status chips use.
 */
function columnFallbackLabel(
  pageContract: AdminUiPageContract,
  column: TaskBoardColumn,
): string {
  if (column === "cancelled") {
    return copy(pageContract, "board.column.cancelled", "Cancelled");
  }
  return copy(pageContract, `board.column.${column}`, column);
}
