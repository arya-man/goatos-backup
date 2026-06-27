import { OperationsAuditPage } from "@/features/operations-audit";
import { getAdminWebBootstrap } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / Data Ops -> Audit Log. The `/operations/audit` path is an implementation detail; the visible IA
// places this business Audit Log under Admin / Data Ops, not under an "Operations" vertical. Cross-surface
// read of every operator/admin/system action that produced built-surface state via the generated client.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const contract = await getAdminWebBootstrap();
  if (!contract.ok) {
    throw new Error(`Admin-web contract unavailable for audit-log: ${contract.error.code ?? contract.error.kind}`);
  }
  const pageContract = contract.data.pages.find((item) => item.route_id === "audit-log");
  if (!pageContract) throw new Error("admin-web page contract missing route_id=audit-log");
  return <OperationsAuditPage searchParams={await searchParams} pageContract={pageContract} roleLenses={contract.data.role_lenses} />;
}
