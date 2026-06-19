import { MortalityDashboardPage } from "@/features/mortality-dashboard";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <MortalityDashboardPage searchParams={await searchParams} />;
}
