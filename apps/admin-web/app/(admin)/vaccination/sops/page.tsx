import { renderSopModulePage } from "@/features/sops/module-page";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Preventive Care (PC) › Vaccination SOP — module-surface SOP page (SOP split, maintainer decision
// 2026-08-18). The retired top-level /sops SOP Library is replaced by this page plus /counts/sops
// and /feed/sops.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("vaccination-sops", "vaccination", "/vaccination/sops", searchParams);
}
