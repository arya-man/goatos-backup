import { LocationsPage } from "@/features/locations";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <LocationsPage searchParams={await searchParams} />;
}
