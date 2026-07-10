import { HRMSPage } from "@/features/people";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / Data Ops / HR / People — Roster and Timetable for workforce management.
// Includes Position & Coverage and Timetable tabs for operational workforce scheduling.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const params = await searchParams;
  const tab = one(params, "tab") ?? "positions";
  return <HRMSPage searchParams={params} tab={tab} pageContract={await requireAdminWebPageContract("people")} />;
}
