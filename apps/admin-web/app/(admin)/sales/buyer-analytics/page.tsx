import { SalesBuyerAnalyticsPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Buyer analytics — who the farm sells to, one row per buyer (maintainer request 2026-09-15):
// contact, category, purchases so far, animals, revenue, whether they come back, and what they
// still owe. Read-only; entry is Sales Config and Vendors.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <SalesBuyerAnalyticsPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("sales-buyer-analytics")}
    />
  );
}
