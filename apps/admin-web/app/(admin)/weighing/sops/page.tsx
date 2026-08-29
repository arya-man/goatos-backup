import { renderSopModulePage } from "@/features/sops";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Weighing › Weighing SOP — the scan-and-submit session document (SOP split extension,
// maintainer decision 2026-08-22, same shape as the three 2026-08-18 routes).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("weighing-sops", "weighing", "/weighing/sops", searchParams);
}
