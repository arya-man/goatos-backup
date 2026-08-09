import { ShedExecutionDetailPage } from "@/features/vaccination-execution/shed-drilldown";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope } from "@/lib/scope";

export const dynamic = "force-dynamic";

export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ shedId: string }>;
  searchParams?: Promise<RouteSearchParams>;
}) {
  const [{ shedId }, spRaw, pageContract] = await Promise.all([
    params,
    searchParams,
    requireAdminWebPageContract("shed-execution"),
  ]);
  const sp = spRaw ?? {};
  const scope = parseScope(sp);
  return (
    <ShedExecutionDetailPage
      shedId={shedId}
      partitionLabel={one(sp, "partition_label")}
      scope={scope}
      asOf={backendScope(scope).asOf}
      pageContract={pageContract}
    />
  );
}
