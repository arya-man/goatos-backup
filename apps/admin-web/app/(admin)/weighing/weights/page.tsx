import { WeighingWeightsPage } from "@/features/weighing";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <WeighingWeightsPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("weighing-weights")}
    />
  );
}
