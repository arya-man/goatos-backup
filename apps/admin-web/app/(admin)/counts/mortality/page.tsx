import { MortalityPage } from "@/features/counts";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Counts -> Mortality. Deaths in a window from every angle the farm asks, beside the
// live head count for every rate.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("counts-mortality")]);
  return <MortalityPage searchParams={params} pageContract={pageContract} />;
}
