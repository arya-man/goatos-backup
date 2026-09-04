import { FeedPurchasesPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Feed Purchases — the buying side of the feed chain: what feed was bought for CBE and CPT, at what
// landed cost, from whom, and whether it has been paid for. These loads are what the stock and
// days-left cards on Feed Analytics are counted from.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("feed-purchases")]);
  return (
    <FeedPurchasesPage
      searchParams={params}
      pageContract={pageContract}
    />
  );
}
