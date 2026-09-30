import { TimetablePage } from "@/features/people";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// People / HRMS > Timetable (maintainer request 2026-09-30): pick a park, see that park's shifts
// and their working hours, and everyone who works there with their shift. HR and the CEO/CXO
// change it; everyone else who can open the page sees the edit controls disabled with a reason.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("people-timetable")]);
  return <TimetablePage searchParams={params} pageContract={pageContract} />;
}
