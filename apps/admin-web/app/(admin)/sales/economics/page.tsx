import { EconomicsPage } from "@/features/sales";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Sales → Economics — the core of the business, animal by animal: daily feed cost, measured
// gain, cost per kg of gain, and what a kg actually sells for.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <EconomicsPage searchParams={await searchParams} pageContract={await requireAdminWebPageContract("sales-economics")} />
  );
}
