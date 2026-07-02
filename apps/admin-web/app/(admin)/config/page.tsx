import { ConfigProtocolRulesPage } from "@/features/config";
import { requireAdminWebPageContract } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / Data Ops / Config — the primary generic protocol-rule authority screen.
// Preventive Care (PC) / Vaccination links here with category=vaccination, but Config is not owned by Vaccination.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const params = await searchParams;
  const category = one(params, "category") ?? "vaccination";
  return <ConfigProtocolRulesPage searchParams={params} category={category} pageContract={await requireAdminWebPageContract("config")} />;
}
