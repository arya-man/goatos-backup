import { MarketAnalyticsPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Market analytics — what goat and sheep fetch in the markets the procurement director phones
// each morning (maintainer decision 2026-09-14): the latest price per city and question, and each
// one over time. Read-only; entry is the phone's Market tab, config is Sales Config.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <MarketAnalyticsPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("sales-market-analytics")}
    />
  );
}
