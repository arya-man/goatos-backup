import { PeopleNotificationsPage } from "@/features/people";
import { requireAdminWebPageContract } from "@/lib/api/server";

export const dynamic = "force-dynamic";

// People / HRMS > Notifications (maintainer decision 2026-09-08; its own page since 2026-09-30):
// which job titles hear each alert. The write is the edit_notifications control.
export default async function Page() {
  const pageContract = await requireAdminWebPageContract("people-notifications");
  return <PeopleNotificationsPage pageContract={pageContract} />;
}
