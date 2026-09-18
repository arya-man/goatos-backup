import { renderSopModulePage } from "@/features/sops";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Configuration › Work instructions — GENERAL SOPs (SOP studio phase 2, maintainer instruction
// 2026-09-18): farm-wide work tied to no module, started by hand from the phone. Same library +
// operator-steps editor as the module SOP pages, scoped to the `general.` slice.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("configuration-work-instructions", "general", "/configuration/work-instructions", searchParams);
}
