import { PeopleVaccinationPage } from "@/features/people";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// People / HRMS > Vaccination operators (its own page since 2026-09-30): each park's vaccination
// operators with their shift, week off and daily animal limit.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("people-vaccination")]);
  return <PeopleVaccinationPage searchParams={params} pageContract={pageContract} />;
}
