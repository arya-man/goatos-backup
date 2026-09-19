import { WeighingWeightsAnalyticsPage } from "@/features/weighing";
import { getAdminWebBootstrap } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";
import { redirect } from "next/navigation";

export const dynamic = "force-dynamic";

const PAGE_PATH = "/weighing/analytics";

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
  throw new Error(`Admin-web contract unavailable for weighing-analytics: ${contract.error.code ?? contract.error.kind}`);
}

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, contract] = await Promise.all([searchParams, getAdminWebBootstrap()]);
  assertContractAvailable(contract);
  const pageContract = contract.data.pages.find((item) => item.route_id === "weighing-analytics");
  if (!pageContract) redirect(firstEnabledPublishedHref(contract) ?? "/");
  return (
    <WeighingWeightsAnalyticsPage
      searchParams={params}
      pageContract={pageContract}
    />
  );
}
