import { LeadershipTasksPage } from "@/features/leadership-tasks/leadership-tasks-page";
import { listLeadershipTasks, requireAdminWebPageContract } from "@/lib/api/server";

export const dynamic = "force-dynamic";

export default async function Page() {
  const [, page] = await Promise.all([
    requireAdminWebPageContract("leadership-tasks"),
    listLeadershipTasks({ scope: "team_progress", filter: "all", limit: 50 }),
  ]);
  return <LeadershipTasksPage page={page.ok ? page.data : null} />;
}
