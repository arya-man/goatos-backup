import { LeadershipTasksPage, parseTasksParams } from "@/features/leadership-tasks";
import { tablePageSizes } from "@/lib/admin-ui-contract";
import {
  listLeadershipTaskAssignees,
  getLeadershipTask,
  listLeadershipTasks,
  requireAdminWebPageContract,
} from "@/lib/api/server";

export const dynamic = "force-dynamic";

/**
 * The Tasks route. It threads the URL state into the list read and hands the SAME search params to
 * the feature, which owns every link it renders.
 *
 * The top-bar scope keys (park / as_of / range / scope_mode / date_from / date_to) are NOT read
 * here — `scripts/check-ia-guard.mjs` forbids a page re-reading the shell's scope, and the task
 * scope this screen pages through is the leadership scope from the shell contract
 * (`assigned_to_me` / `assigned_by_me` / `team_progress`), which is a different thing that happens
 * to share the word.
 */
export default async function Page({
  searchParams,
}: {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
  const sp = (await searchParams) ?? {};
  const pageContract = await requireAdminWebPageContract("leadership-tasks");
  const params = parseTasksParams(sp, tablePageSizes(pageContract, "leadership-task-progress"));
  const listQuery = {
    scope: params.scope,
    // Every one of these is already normalized by parseTasksParams: a malformed uuid, an unknown
    // sort, an over-long `q` and a HALF date range are dropped rather than sent, because the list
    // endpoint answers 400 for each of them and a stale bookmark should degrade to the unfiltered
    // list instead of an error page.
    q: params.q,
    assigneeUserId: params.assigneeUserID,
    raisedBy: params.raisedBy,
    deadlineFrom: params.deadline.incomplete ? undefined : params.deadline.from,
    deadlineTo: params.deadline.incomplete ? undefined : params.deadline.to,
    raisedFrom: params.raised.incomplete ? undefined : params.raised.from,
    raisedTo: params.raised.incomplete ? undefined : params.raised.to,
    sort: params.sort,
  } as const;
  // CANCELLED TASKS ARE LISTED (maintainer decision 2026-09-25). The unfiltered list does not
  // carry them (team progress excludes cancelled rows), so the board's Cancelled column is filled
  // by the list endpoint's own `cancelled` filter: the first page of it, beside the first page of
  // the board, in parallel. A backend without that filter refuses it and the column stays empty
  // with no count -- never a number invented on this side.
  // Read on either view: Board / List is client-local presentation (no route render on switch), so
  // a list-first load that flips to the board must already hold the column's rows.
  const wantCancelledColumn = params.filter === "all" && params.page === 1;
  const [page, assignees, detail, cancelled] = await Promise.all([
    listLeadershipTasks({ ...listQuery, filter: params.filter, limit: params.limit, cursor: params.cursor }),
    listLeadershipTaskAssignees(),
    // The drawer's full record (notes + activity) rides its own read, in parallel, so the list
    // payload stays a list. Absent `task=` this is a resolved null, not a request.
    params.selectedTaskID ? getLeadershipTask(params.selectedTaskID) : Promise.resolve(null),
    wantCancelledColumn
      ? // request-plan:ignore owner=admin-web issue=tasks-cancelled-column expires=2027-03-31 reason=DISJOINT not overlapping: filter=cancelled returns only rows the unfiltered read excludes; one extra bounded page (same limit), first page only; remove if the list read ever returns the cancelled column itself
        listLeadershipTasks({ ...listQuery, filter: "cancelled", limit: params.limit })
      : Promise.resolve(null),
  ]);
  return (
    <LeadershipTasksPage
      page={page.ok ? page.data : null}
      pageContract={pageContract}
      searchParams={sp}
      assignees={assignees.ok ? assignees.data.assignees : []}
      selectedTask={detail && detail.ok ? detail.data.task : null}
      cancelledRows={cancelled && cancelled.ok ? cancelled.data.rows : []}
    />
  );
}
