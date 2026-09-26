import { ControlTowerPage } from "@/features/control-tower";
import { landingWindow, weightsWindowSettings, WINDOW_FROM_PARAM, WINDOW_TO_PARAM } from "@/features/weighing";
import { getAdminWebBootstrap } from "@/lib/api/server";
import { originFromParam } from "@/lib/animal-origin";
import { todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import { redirect } from "next/navigation";
import { hrefWithWindow } from "./landing-href.mjs";

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
// sidebar and renders only through its explicit /?lens=control-tower deep link. A
// principal whose contract carries no ADG page keeps the previous Control Tower / fallback behaviour.
const LANDING_ROUTE_ID = "weighing-analytics";
const CONTROL_TOWER_LENS = "control-tower";

function weighingModeFilter(raw: string | undefined): string {
  return raw === "individual_animal" || raw === "per_shed_partition" ? raw : "all";
}

async function landingHref(landing: { href: string; copy?: Record<string, string> }, params: RouteSearchParams): Promise<string> {
  if (landing.href !== "/weighing/analytics") return scopeHref(landing.href, parseScope(params));
  const selectedFrom = one(params, WINDOW_FROM_PARAM);
  const selectedTo = one(params, WINDOW_TO_PARAM);
  if (selectedFrom && selectedTo) return hrefWithWindow(landing.href, params, selectedFrom, selectedTo);

  const rawSex = one(params, "sex");
  const sexFilter = rawSex === "female" ? "female" : rawSex === "all" ? "" : "male";
  const originFilter = originFromParam(one(params, "origin"));
  const modeFilter = weighingModeFilter(one(params, "weighing"));
  const weighingCategoryFilter = modeFilter !== "all" ? modeFilter : "";
  const today = todayIso();
  const windowSettings = weightsWindowSettings(landing.copy, today);
  const window = await landingWindow(
    params,
    today,
    one(params, "park") ?? "",
    sexFilter,
    originFilter,
    weighingCategoryFilter,
    windowSettings,
  );
  return hrefWithWindow(landing.href, params, window.from, window.to);
}

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [sp, contract] = await Promise.all([searchParams, getAdminWebBootstrap()]);
  const requestedControlTower = one(sp, "lens") === CONTROL_TOWER_LENS;
  const landing = contract.ok ? contract.data.pages.find((item) => item.route_id === LANDING_ROUTE_ID) : null;
  if (!requestedControlTower && landing?.href) {
    redirect(await landingHref(landing, sp));
  }
  const controlTower = contract.ok ? contract.data.pages.find((item) => item.route_id === "control-tower") : null;
  if (requestedControlTower && controlTower) {
    return <ControlTowerPage searchParams={sp} pageContract={controlTower} />;
  }
  if (controlTower?.href && !landing?.href) {
    redirect(scopeHref("/", parseScope(sp), {}, { lens: CONTROL_TOWER_LENS }));
  }
  redirect(firstEnabledPublishedHref(contract) ?? "/vaccination");
}
