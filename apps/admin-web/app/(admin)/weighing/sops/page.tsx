import { renderSopModulePage } from "@/features/sops";
import { WeightsAssumptionsControl } from "@/features/weighing";
import { controlEnabled } from "@/lib/admin-ui-contract";
import { getGrowthAssumptions } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

const PAGE_PATH = "/weighing/sops";

// Weighing › Weighing SOP — the scan-and-submit session document (SOP split extension,
// maintainer decision 2026-08-22, same shape as the three 2026-08-18 routes).
//
// It also carries the Assumptions drawer (maintainer decision 2026-09-19): the figures the Weighing
// pages are valued and judged against, edited beside the rules they belong to. Rendered when the
// page contract enables `edit_assumptions` -- "who have [the tick] should only see it" -- so a reader
// without the write sees no button at all.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("weighing-sops", "weighing", PAGE_PATH, searchParams, async (pageContract, sp) => {
    if (!controlEnabled(pageContract, "edit_assumptions", false)) return null;
    const assumptions = await getGrowthAssumptions();
    if (!assumptions.ok) return null;
    const open = new URLSearchParams();
    for (const [key, value] of Object.entries(sp)) {
      if (typeof value === "string" && value) open.set(key, value);
    }
    const closeQuery = new URLSearchParams(open);
    closeQuery.delete("wt_assumptions");
    open.set("wt_assumptions", "1");
    return (
      <WeightsAssumptionsControl
        pageContract={pageContract}
        assumptions={assumptions.data}
        openHref={`${PAGE_PATH}?${open.toString()}`}
        closeHref={closeQuery.size ? `${PAGE_PATH}?${closeQuery.toString()}` : PAGE_PATH}
      />
    );
  });
}
