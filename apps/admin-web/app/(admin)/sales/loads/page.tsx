import { SalesLoadsPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Purchase & barn — every batch of animals reconciled: what was bought or born, what sold, what
// died, what is still on farm, and the money on each side.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <SalesLoadsPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("sales-loads")}
    />
  );
}
