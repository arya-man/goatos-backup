import { ProcurementLoadDetailPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Load Detail — full journey timeline, per-goat rows, pre-dispatch decisions, and the arrival gate.
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ load_id: string }>;
  searchParams: Promise<RouteSearchParams>;
}) {
  const [{ load_id }, sp, pageContract] = await Promise.all([
    params,
    searchParams,
    requireAdminWebPageContract("source-load"),
  ]);
  return (
    <ProcurementLoadDetailPage
      loadId={decodeURIComponent(load_id)}
      searchParams={sp}
      pageContract={pageContract}
    />
  );
}
