import { VaccinationCalendarPage } from "@/features/calendar";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Calendar is a top-level command lens. Vaccination-only is the data scope, not the UI hierarchy.
// The visible slice is Preventive Care (PC) Vaccination due work; see
// context/execution/calendar-vaccination-slice-parallel-handoff.md.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <VaccinationCalendarPage searchParams={await searchParams} pageContract={await requireAdminWebPageContract("calendar")} />;
}
