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
  const { shedId } = await params;
  const sp = (await searchParams) ?? {};
  const scope = parseScope(sp);
  const pageContract = await requireAdminWebPageContract("shed-execution");
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
