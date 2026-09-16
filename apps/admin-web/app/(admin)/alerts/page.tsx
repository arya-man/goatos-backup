import { AlertsPage } from "@/features/alerts";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Alerts — what is off today across the parks the caller may see, directly below the Work Board.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("alerts")]);
  return <AlertsPage searchParams={params} pageContract={pageContract} />;
}
