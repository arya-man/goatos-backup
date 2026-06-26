import { VaccinationWorkflowDrilldownPage } from "@/features/process-integrity";
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
  const { row_id } = await params;
  return <VaccinationWorkflowDrilldownPage rowId={decodeURIComponent(row_id)} searchParams={await searchParams} />;
}
