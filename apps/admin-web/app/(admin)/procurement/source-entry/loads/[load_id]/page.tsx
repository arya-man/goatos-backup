import { ProcurementLoadDetailPage } from "@/features/procurement";
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
  const { load_id } = await params;
  return <ProcurementLoadDetailPage loadId={decodeURIComponent(load_id)} searchParams={await searchParams} />;
}
