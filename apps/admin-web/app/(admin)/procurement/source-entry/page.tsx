import { SourceEntryBoardPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Source Entry Board — procurement vertical home. Loads from purchase/source through accepted intake.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("source-entry")]);
  return <SourceEntryBoardPage searchParams={params} pageContract={pageContract} />;
}
