import { SalesFarmValuePage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Farm value — what the live herd is worth today at Sales target rates: total farm value, total
// meat, the Over 35 kg sale-ready count with its error margin, and the by-category breakdown.
// Split out of the Sales board (maintainer decision 2026-09-11).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <SalesFarmValuePage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("sales-farm-value")}
    />
  );
}
