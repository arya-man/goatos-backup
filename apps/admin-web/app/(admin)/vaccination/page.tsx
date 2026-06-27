import { VaccinationOperationsPage } from "@/features/phc-vaccination";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// PHC · Vaccination is the operations surface (due drives, sessions, proof + verification queues). It is NOT
// the Action Center — that, Protocol Adherence, and Workflows are top-level command screens linked from here.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <VaccinationOperationsPage searchParams={await searchParams} pageContract={await requireAdminWebPageContract("vaccination")} />;
}
