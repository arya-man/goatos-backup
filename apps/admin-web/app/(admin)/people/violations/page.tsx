import { ViolationsPage } from "@/features/discipline";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// People / HRMS > Violations (maintainer decisions 2026-09-30): violations recorded against each
// person with a fine in rupees; HR and the CEO/CXO record and withdraw here.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("people-violations")]);
  return <ViolationsPage searchParams={params} pageContract={pageContract} />;
}
