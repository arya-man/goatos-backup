import { redirect } from "next/navigation";

import { legacyPeopleTabRedirect, PeoplePage } from "@/features/people";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// People / HRMS > People — the all-people staff directory and the Add Person onboarding drawer
// that creates the login account from the app. The views that used to sit behind this page's tab
// strip are HRMS pages of their own since 2026-09-30; an old ?tab= link is sent to its page.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const params = await searchParams;
  const moved = legacyPeopleTabRedirect(params);
  if (moved) redirect(moved);
  const pageContract = await requireAdminWebPageContract("people");
  return <PeoplePage searchParams={params} pageContract={pageContract} />;
}
