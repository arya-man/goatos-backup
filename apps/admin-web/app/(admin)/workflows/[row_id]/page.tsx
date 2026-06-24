import { VaccinationWorkflowDrilldownPage } from "@/features/process-integrity";

export const dynamic = "force-dynamic";

// Workflow drilldown for one vaccination obligation/drive — the chain-reaction map. Top-level route.
export default async function Page({ params }: { params: Promise<{ row_id: string }> }) {
  const { row_id } = await params;
  return <VaccinationWorkflowDrilldownPage rowId={decodeURIComponent(row_id)} />;
}
