import { HerdSearchPage } from "@/features/herd-search";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <HerdSearchPage searchParams={await searchParams} />;
}
