import { redirect } from "next/navigation";

import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// The Sales board is RETIRED (maintainer decision 2026-09-11): its blocks were divided into Sold
// (/sales/sold, with the deals ledger last) and Farm value (/sales/farm-value). This route stays
// only so an old link or bookmark lands on Sold with its query intact, never on a 404.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(await searchParams)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  const qs = query.toString();
  redirect(qs ? `/sales/sold?${qs}` : "/sales/sold");
}
