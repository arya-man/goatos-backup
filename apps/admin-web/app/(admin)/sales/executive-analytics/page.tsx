import { SalesExecutiveAnalyticsPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Sales executive analytics — what the sales desk is doing (maintainer request 2026-10-02):
// vendors added and changed, calls, sales and payments recorded, by person and by day. Read-only.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <SalesExecutiveAnalyticsPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("sales-executive-analytics")}
    />
  );
}
