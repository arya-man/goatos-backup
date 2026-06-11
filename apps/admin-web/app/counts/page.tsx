import { IdentityCountsPage } from "@/features/identity-counts";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <IdentityCountsPage searchParams={await searchParams} />;
}
