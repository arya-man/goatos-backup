import { AnimalPurchasesPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Animal purchases — the loads on offer and the animals the buying desk filmed in them. The
// CEO/CXO watches each video here and accepts or rejects the animal; the desk sees the answer on
// the phone at once (maintainer decision 2026-09-13).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("animal-purchases")]);
  return (
    <AnimalPurchasesPage
      searchParams={params}
      pageContract={pageContract}
    />
  );
}
