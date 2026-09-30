import { PeopleClockPage } from "@/features/people";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// People / HRMS > Clock In / Out (maintainer decisions 2026-08-27/28; its own page since
// 2026-09-30). Gated on the directory read AND clock.presence.read.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("people-clock")]);
  return <PeopleClockPage searchParams={params} pageContract={pageContract} />;
}
