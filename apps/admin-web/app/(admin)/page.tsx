import { ControlTowerPage } from "@/features/control-tower";
import { getAdminWebBootstrap } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";
import { redirect } from "next/navigation";

export const dynamic = "force-dynamic";

function firstEnabledPublishedHref(contract: Awaited<ReturnType<typeof getAdminWebBootstrap>>): string | null {
  if (!contract.ok) return null;
  const pageHrefs = new Set(contract.data.pages.map((page) => page.href));
  const candidates = [
    ...contract.data.navigation.primary,
    ...contract.data.navigation.groups.flatMap((group) => group.leaves),
  ];
  const enabledPublished = candidates.filter((item) => item.enabled && item.href !== "/" && pageHrefs.has(item.href));
  return enabledPublished.find((item) => item.href === "/verify")?.href ?? enabledPublished[0]?.href ?? null;
}

// ADG Analytics is the landing page (maintainer request 2026-09-09): whoever holds its
// page contract lands there after login and on "/". Control Tower is parked from the
// sidebar and only renders here for a principal whose contract carries no ADG page (the
// verifier lens, for one, drops every module page and keeps its own landing).
const LANDING_ROUTE_ID = "weighing-analytics";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, contract] = await Promise.all([searchParams, getAdminWebBootstrap()]);
  const landing = contract.ok ? contract.data.pages.find((item) => item.route_id === LANDING_ROUTE_ID) : null;
  if (landing?.href) {
    redirect(landing.href);
  }
  const controlTower = contract.ok ? contract.data.pages.find((item) => item.route_id === "control-tower") : null;
  if (!controlTower) {
    redirect(firstEnabledPublishedHref(contract) ?? "/vaccination");
  }
  return <ControlTowerPage searchParams={sp} pageContract={controlTower} />;
}
