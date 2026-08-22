import { renderSopModulePage } from "@/features/sops";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Milk › Milk SOP — preparation / feeding documents (SOP split extension, maintainer decision
// 2026-08-22, same shape as the three 2026-08-18 routes).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("milk-sops", "milk", "/milk/sops", searchParams);
}
