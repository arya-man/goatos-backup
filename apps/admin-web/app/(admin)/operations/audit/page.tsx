import { OperationsAuditPage } from "@/features/operations-audit";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Operations -> Audit Log. Cross-operations read surface for every operator/admin/system action that
// produced vaccination and other process-integrity state through the generated admin client.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <OperationsAuditPage searchParams={await searchParams} />;
}
