import { LeadershipTasksPage, parseTasksParams } from "@/features/leadership-tasks";
import { tablePageSizes } from "@/lib/admin-ui-contract";
import {
  listLeadershipTaskAssignees,
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
  const [page, assignees] = await Promise.all([
    listLeadershipTasks({
      scope: params.scope,
      filter: params.filter,
      limit: params.limit,
      cursor: params.cursor,
      // Every one of these is already normalized by parseTasksParams: a malformed uuid, an
      // unknown sort, an over-long `q` and a HALF date range are dropped rather than sent, because
      // the list endpoint answers 400 for each of them and a stale bookmark should degrade to the
      // unfiltered list instead of an error page.
      q: params.q,
      assigneeUserId: params.assigneeUserID,
      raisedBy: params.raisedBy,
      deadlineFrom: params.deadline.incomplete ? undefined : params.deadline.from,
      deadlineTo: params.deadline.incomplete ? undefined : params.deadline.to,
      raisedFrom: params.raised.incomplete ? undefined : params.raised.from,
      raisedTo: params.raised.incomplete ? undefined : params.raised.to,
      sort: params.sort,
    }),
    listLeadershipTaskAssignees(),
  ]);
  return (
    <LeadershipTasksPage
      page={page.ok ? page.data : null}
      pageContract={pageContract}
      searchParams={sp}
      assignees={assignees.ok ? assignees.data.assignees : []}
    />
  );
}
