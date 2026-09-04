import { SalesPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Sales — animal and manure sales across both farms: revenue, buyers, demand pipeline, sale
// evidence, and the deals ledger.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("sales")]);
  return <SalesPage searchParams={params} pageContract={pageContract} />;
}
