import { LeavePage } from "@/features/leave";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Leave (maintainer decisions 2026-09-10, docs/features/leave-requests/plan.md): the park head +
// HR approval queue for leave raised from the phone Clock screen, the People / HRMS list of every
// request, and the CEO-only "who approves" flags. Renders from the backend page contract
// (route_id "leave"); the sidebar leaf is gated on leave.approve and every write route enforces
// its own permission independently.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("leave")]);
  return <LeavePage searchParams={params} pageContract={pageContract} />;
}
