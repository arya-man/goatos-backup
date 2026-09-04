import { VaccinationWorkflowsPage } from "@/features/process-integrity";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Workflows is a top-level command screen. Vaccination-only is the data scope, not the UI hierarchy.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("workflows")]);
  return <VaccinationWorkflowsPage searchParams={params} pageContract={pageContract} />;
}
