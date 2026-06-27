import { VaccinationCalendarPage } from "@/features/calendar";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Calendar is a top-level command lens. Vaccination-only is the data scope, not the UI hierarchy.
// The visible slice is PHC Vaccination due work; see
// context/execution/calendar-vaccination-slice-parallel-handoff.md.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <VaccinationCalendarPage searchParams={await searchParams} />;
}
