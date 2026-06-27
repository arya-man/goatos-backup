import { ShedExecutionDetailPage } from "@/features/vaccination-execution";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";
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
  const { asOf } = backendScope(parseScope(sp));
  return <ShedExecutionDetailPage shedId={shedId} asOf={asOf} pageContract={await requireAdminWebPageContract("shed-execution")} />;
}
