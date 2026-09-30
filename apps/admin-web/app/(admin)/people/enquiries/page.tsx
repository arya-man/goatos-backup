import { EnquiriesPage } from "@/features/discipline";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// People / HRMS > Enquiries (maintainer decisions 2026-09-30): opened by farm events (first: an
// approved death); the park head fills each on the phone, HR here.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("people-enquiries")]);
  return <EnquiriesPage searchParams={params} pageContract={pageContract} />;
}
