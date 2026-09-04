import { VendorBoardPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Vendors — the procurement register of every counterparty the farm buys from or contracts with.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("vendors")]);
  return <VendorBoardPage searchParams={params} pageContract={pageContract} />;
}
