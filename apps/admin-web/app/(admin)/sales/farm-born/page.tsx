import { SalesFarmBornPage } from "@/features/procurement";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Farm born — the animals born on this farm (maintainer request 2026-09-18): on the
// farm today, sold in the chosen period, by breed / sex / stage / pen, and what they earned.
// Read-only; entry is Sales Config.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return (
    <SalesFarmBornPage
      searchParams={await searchParams}
      pageContract={await requireAdminWebPageContract("sales-farm-born")}
    />
  );
}
