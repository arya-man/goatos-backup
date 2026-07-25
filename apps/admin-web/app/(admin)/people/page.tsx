import { HRMSPage } from "@/features/people";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Vaccination Operators HRMS — Merged unified screen with roster, availability, leave management,
// and drive-operator assignment, all on one page.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const params = await searchParams;
  return <HRMSPage searchParams={params} tab="" pageContract={await requireAdminWebPageContract("people")} />;
}
