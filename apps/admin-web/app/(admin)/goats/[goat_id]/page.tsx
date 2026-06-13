import { GoatPassportPage } from "@/features/goat-passport";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ params, searchParams }: { params: Promise<{ goat_id: string }>; searchParams: Promise<RouteSearchParams> }) {
  const { goat_id } = await params;
  return <GoatPassportPage goatId={goat_id} searchParams={await searchParams} />;
}
