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
  return enabledPublished.find((item) => item.href === "/actions")?.href ?? enabledPublished[0]?.href ?? null;
}

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, contract] = await Promise.all([searchParams, getAdminWebBootstrap()]);
  const controlTower = contract.ok ? contract.data.pages.find((item) => item.route_id === "control-tower") : null;
  if (!controlTower) {
    redirect(firstEnabledPublishedHref(contract) ?? "/vaccination");
  }
  return <ControlTowerPage searchParams={sp} pageContract={controlTower} />;
}
