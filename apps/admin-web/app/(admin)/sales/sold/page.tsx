import { SalesSoldPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Sold — what has already left the farm: revenue, animals, sold weight bands, price per kg by
// breed, buyers, the demand pipeline and sale evidence. Split out of the Sales board (maintainer
// decision 2026-09-11).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <SalesSoldPage searchParams={await searchParams} pageContract={await requireAdminWebPageContract("sales-sold")} />
  );
}
