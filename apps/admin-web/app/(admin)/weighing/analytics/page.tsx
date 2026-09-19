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

function ContractUnavailable({ contract }: { contract: Awaited<ReturnType<typeof getAdminWebBootstrap>> }) {
  if (contract.ok) return null;
  return (
    <section className="card">
      <h1>Admin-web contract unavailable</h1>
      <p className="muted">
        The backend-owned UI contract could not be loaded, so ADG Analytics is not rendering local fallback IA.
        Resolve the API/session/tenant error and reload.
      </p>
      <div className="metagrid" style={{ marginTop: 14 }}>
        <div>
          <div className="k">Error</div>
          <div className="v">{contract.error.code ?? contract.error.kind}</div>
        </div>
        <div>
          <div className="k">Detail</div>
          <div className="v">{contract.error.message}</div>
        </div>
      </div>
    </section>
  );
}

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, contract] = await Promise.all([searchParams, getAdminWebBootstrap()]);
  if (!contract.ok) return <ContractUnavailable contract={contract} />;
  const pageContract = contract.data.pages.find((item) => item.route_id === "weighing-analytics");
  if (!pageContract) redirect(firstEnabledPublishedHref(contract) ?? "/");
  return (
    <WeighingWeightsAnalyticsPage
      searchParams={params}
      pageContract={pageContract}
    />
  );
}
