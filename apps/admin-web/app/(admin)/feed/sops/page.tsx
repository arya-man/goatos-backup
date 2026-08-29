import { renderSopModulePage } from "@/features/sops";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Feed › Feed SOP — distribution / packing / transport documents (SOP split, maintainer decision
// 2026-08-18).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("feed-sops", "feed", "/feed/sops", searchParams);
}
