import { SalesLoadsPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Purchase and Born — every batch of animals reconciled: what was bought or born, what sold, what
// died, what is still on farm, and the money on each side.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("sales-loads")]);
  return (
    <SalesLoadsPage
      searchParams={params}
      pageContract={pageContract}
    />
  );
}
