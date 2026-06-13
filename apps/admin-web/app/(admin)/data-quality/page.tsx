import { DataQualityPage } from "@/features/data-quality";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <DataQualityPage searchParams={await searchParams} />;
}
