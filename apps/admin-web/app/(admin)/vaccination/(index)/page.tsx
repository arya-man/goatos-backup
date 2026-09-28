import { VaccinationOperationsPage } from "@/features/preventive-care-vaccination";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Preventive Care (PC) · Vaccination is the operations surface (due drives, sessions, proof + verification queues). It is NOT
// the Action Center — that, Protocol Adherence, and Workflows are top-level command screens linked from here.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("vaccination")]);
  return <VaccinationOperationsPage searchParams={params} pageContract={pageContract} />;
}
