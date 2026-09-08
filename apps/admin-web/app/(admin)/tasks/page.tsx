import { LeadershipTasksPage } from "@/features/leadership-tasks/leadership-tasks-page";
import { requireAdminWebPageContract } from "@/lib/api/server";

export const dynamic = "force-dynamic";

export default async function Page() {
  await requireAdminWebPageContract("leadership-tasks");
  return <LeadershipTasksPage />;
}
