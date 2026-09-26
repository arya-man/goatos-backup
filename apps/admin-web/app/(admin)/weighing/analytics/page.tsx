import { WeighingWeightsAnalyticsPage, landingWindow, WINDOW_FROM_PARAM, WINDOW_TO_PARAM } from "@/features/weighing";
import { getAdminWebBootstrap } from "@/lib/api/server";
import { CanonicalUrl } from "@/components/canonical-url";
import { todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { redirect } from "next/navigation";

export const dynamic = "force-dynamic";

const PAGE_PATH = "/weighing/analytics";
const ROUTE_ID = "weighing-analytics";
/** Set by proxy.ts when "/" was rewritten here, so a principal without this page can fall back. */
const ROOT_FALLBACK_PARAM = "from_root";

function weighingModeFilter(raw: string | undefined): string {
  return raw === "individual_animal" || raw === "per_shed_partition" ? raw : "all";
}

function firstEnabledPublishedHref(contract: Awaited<ReturnType<typeof getAdminWebBootstrap>>): string | null {
  if (!contract.ok) return null;
  const pageHrefs = new Set(contract.data.pages.map((page) => page.href));
  const candidates = [
    ...contract.data.navigation.primary,
    ...contract.data.navigation.groups.flatMap((group) => group.leaves),
  ];
  const enabledPublished = candidates.filter((item) => item.enabled && item.href !== PAGE_PATH && pageHrefs.has(item.href));
  return enabledPublished[0]?.href ?? null;
}

function assertContractAvailable(
  contract: Awaited<ReturnType<typeof getAdminWebBootstrap>>,
): asserts contract is Extract<Awaited<ReturnType<typeof getAdminWebBootstrap>>, { ok: true }> {
  if (contract.ok) return;
  // Thrown to the route's error boundary (never a redirect): the boundary owns the retry copy.
  const reason = contract.error.code ?? contract.error.kind;
  throw new Error(`Weighing analytics page could not be loaded (${reason})`);
}

function hrefWithWindow(params: RouteSearchParams, from: string, to: string): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (Array.isArray(value)) value.forEach((item) => next.append(key, item));
    else if (value) next.set(key, value);
  }
  next.set(WINDOW_FROM_PARAM, from);
  next.set(WINDOW_TO_PARAM, to);
  return `${PAGE_PATH}?${next.toString()}`;
}

/**
 * The canonical window for a request that named none. Computed HERE and rendered in the same
 * document (the URL is then settled in place by <CanonicalUrl>), never via redirect(): a streamed
 * redirect under the route's loading boundary meant two skeletons on every visit to "/" (the
 * shell's, then this route's on the redirected document).
 */
async function canonicalWindowParams(params: RouteSearchParams): Promise<RouteSearchParams> {
  if (one(params, WINDOW_FROM_PARAM) || one(params, WINDOW_TO_PARAM)) return params;

  const rawSex = one(params, "sex");
  const sexFilter = rawSex === "female" ? "female" : rawSex === "all" ? "" : "male";
  const rawOrigin = one(params, "origin");
  const originFilter = rawOrigin === "farm_born" || rawOrigin === "purchased" ? rawOrigin : "";
  const modeFilter = weighingModeFilter(one(params, "weighing"));
  const weighingCategoryFilter = modeFilter !== "all" ? modeFilter : "";
  const window = await landingWindow(
    params,
    todayIso(),
    one(params, "park") ?? "",
    sexFilter,
    originFilter,
    weighingCategoryFilter,
  );
  return { ...params, [WINDOW_FROM_PARAM]: window.from, [WINDOW_TO_PARAM]: window.to };
}

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  // The page view of the bootstrap: this route's copy/tables/option groups in full, the rest withheld.
  const [raw, contract] = await Promise.all([searchParams, getAdminWebBootstrap({ kind: "page", routeId: ROUTE_ID })]);
  assertContractAvailable(contract);
  const pageContract = contract.data.pages.find((item) => item.route_id === ROUTE_ID);
  // A principal without this page (possibly sent here by the proxy's root redirect) goes to the
  // first page they do hold, else the root's own fallback landing — never back through the proxy
  // redirect that brought them here.
  if (!pageContract) redirect(firstEnabledPublishedHref(contract) ?? "/?fallback=1");
  const { [ROOT_FALLBACK_PARAM]: _fromRoot, ...cleaned } = raw;
  void _fromRoot;
  // serial-await: allow the window is only worth computing once the principal is known to hold this page (else we bounce).
  const params = await canonicalWindowParams(cleaned);
  return (
    <>
      <CanonicalUrl href={hrefWithWindow(params, one(params, WINDOW_FROM_PARAM) ?? "", one(params, WINDOW_TO_PARAM) ?? "")} />
      <WeighingWeightsAnalyticsPage
        searchParams={params}
        pageContract={pageContract}
      />
    </>
  );
}
