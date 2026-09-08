import { LeadershipTasksPage } from "@/features/leadership-tasks/leadership-tasks-page";
import {
  listLeadershipTaskAssignees,
  listLeadershipTasks,
  requireAdminWebPageContract,
} from "@/lib/api/server";

export const dynamic = "force-dynamic";

type ScopeKey = "assigned_to_me" | "assigned_by_me" | "team_progress";

const scopes: ScopeKey[] = [
  "assigned_to_me",
  "assigned_by_me",
  "team_progress",
];

export default async function Page({
  searchParams,
}: {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = (await searchParams) ?? {};
  const rawScope = Array.isArray(params.scope) ? params.scope[0] : params.scope;
  const scope = scopes.includes(rawScope as ScopeKey)
    ? (rawScope as ScopeKey)
    : "team_progress";
  const [, page, assignees] = await Promise.all([
    requireAdminWebPageContract("leadership-tasks"),
    listLeadershipTasks({ scope, filter: "all", limit: 50 }),
    listLeadershipTaskAssignees(),
  ]);
  return (
    <LeadershipTasksPage
      page={page.ok ? page.data : null}
      assignees={assignees.ok ? assignees.data.assignees : []}
      selectedScopeKey={scope}
    />
  );
}
