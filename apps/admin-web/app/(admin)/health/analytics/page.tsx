import { HealthAnalyticsPage } from "@/features/health";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Health -> Health Analytics. The leadership read: what the herd is being treated for, whether
// the prescribed courses are actually carried out, and what it is dying of.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <HealthAnalyticsPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("health-analytics")}
    />
  );
}
