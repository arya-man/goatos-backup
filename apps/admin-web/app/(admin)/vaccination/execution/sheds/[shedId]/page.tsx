import { VaccinationShedDetailPage } from "@/features/vaccination-sheds";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ shedId: string }>;
  searchParams?: Promise<RouteSearchParams>;
}) {
  const { shedId } = await params;
  const sp = (await searchParams) ?? {};
  const [pageContract, passportPageContract] = await Promise.all([
    requireAdminWebPageContract("shed-execution"),
    requireAdminWebPageContract("goat-passport"),
  ]);
  return <VaccinationShedDetailPage shedId={shedId} searchParams={sp} pageContract={pageContract} passportPageContract={passportPageContract} />;
}
