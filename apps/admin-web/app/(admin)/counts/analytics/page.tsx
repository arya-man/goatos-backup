import { HerdAnalyticsPage } from "@/features/counts";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Counts -> Herd Analytics. The leadership read: what the herd is made of right now,
// beside what changed it month by month.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <HerdAnalyticsPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("herd-analytics")}
    />
  );
}
