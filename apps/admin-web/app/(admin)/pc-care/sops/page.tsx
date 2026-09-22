import { renderSopModulePage } from "@/features/sops";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Preventive Care › Preventive Care SOP — what the operator captures and answers on a deworming,
// ticks removal or trimming task, and whether a tablet-in-feed deworming removes feed and water
// the evening before (PC CARE SOP, maintainer decision 2026-09-22).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("pc-care-sops", "pc_care", "/pc-care/sops", searchParams);
}
