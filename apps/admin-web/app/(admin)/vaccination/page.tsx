import { VaccinationActionCenterPage } from "@/features/vaccination";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <VaccinationActionCenterPage searchParams={await searchParams} />;
}
