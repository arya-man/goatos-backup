import { VaccinationWorkflowDrilldownPage } from "@/features/process-integrity";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Workflow drilldown for one vaccination obligation/drive — the chain-reaction map. Top-level route.
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ row_id: string }>;
  searchParams?: Promise<RouteSearchParams>;
}) {
  const [{ row_id }, sp, pageContract] = await Promise.all([
    params,
    searchParams,
    requireAdminWebPageContract("workflow-record"),
  ]);
  return (
    <VaccinationWorkflowDrilldownPage
      rowId={decodeURIComponent(row_id)}
      searchParams={sp}
      pageContract={pageContract}
    />
  );
}
