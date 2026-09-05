import { VendorBoardPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Vendors under Sales — the SELLING half of the one vendor register: the agents, butchers, farmers,
// slaughter houses and companies the farm sells to (maintainer decision 2026-09-05).
//
// The SAME component as /procurement/vendors, deliberately. It is the same register, the same
// drawer and the same form; only which record types it carries differs, and that is declared in the
// "sales-vendors" page contract rather than chosen here. A second copy of the board would be two
// screens to keep in step for one table.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([
    searchParams,
    requireAdminWebPageContract("sales-vendors"),
  ]);
  return <VendorBoardPage searchParams={params} pageContract={pageContract} />;
}
