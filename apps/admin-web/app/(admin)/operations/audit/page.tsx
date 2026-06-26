import { OperationsAuditPage } from "@/features/operations-audit";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / Data Ops -> Audit Log. The `/operations/audit` path is an implementation detail; the visible IA
// places this business Audit Log under Admin / Data Ops, not under an "Operations" vertical. Cross-surface
// read of every operator/admin/system action that produced built-surface state via the generated client.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <OperationsAuditPage searchParams={await searchParams} />;
}
