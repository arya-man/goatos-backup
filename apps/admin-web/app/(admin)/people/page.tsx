import { PeoplePage } from "@/features/people";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// People / HRMS — the all-people staff directory (default tab) with the module
// staffing views (Vaccination = the operators/leave/timetable screen) behind
// the backend-owned `people_view_tabs` strip, and the Add Person onboarding
// drawer that creates the login account from the app.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const params = await searchParams;
  return <PeoplePage searchParams={params} pageContract={await requireAdminWebPageContract("people")} />;
}
