import { OperationsDLQPage } from "@/features/operations-dlq";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / Data Ops -> DLQ Center. Backend-owned UI contract plus generated admin-api client;
// React owns only layout, local selected-row state through ?dlq_id, and form submission.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("dlq-center")]);
  return <OperationsDLQPage searchParams={params} pageContract={pageContract} />;
}
