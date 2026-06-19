import { CountsDashboardPage } from "@/features/counts-dashboard";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <CountsDashboardPage searchParams={await searchParams} />;
}
