import { renderSopModulePage } from "@/features/sops/module-page";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Counts › Herd Operations SOP — birth / death / shifting documents (SOP split, maintainer
// decision 2026-08-18).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("counts-sops", "counts", "/counts/sops", searchParams);
}
